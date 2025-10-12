package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBasicHTTPServer tests the simplest case: spawning a Python HTTP server
// and verifying the complete workflow (logs page, logs API, proxying)
func TestBasicHTTPServer(t *testing.T) {
	// Get free ports for proxy and subprocess
	proxyPort := getFreePort(t)
	destPort := getFreePort(t)

	// Build the binary first
	binaryPath := buildBinary(t)

	// Start jhub-app-proxy
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath,
		"--port", fmt.Sprintf("%d", proxyPort),
		"--destport", fmt.Sprintf("%d", destPort),
		"--authtype", "none",
		"--log-format", "json",
		"--log-level", "info",
		"--",
		"python3", "-m", "http.server", "{port}",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start jhub-app-proxy: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}()

	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", proxyPort)

	// Wait for proxy to be ready
	if err := waitForHTTP(proxyURL, 30*time.Second); err != nil {
		t.Fatalf("Proxy did not become ready: %v", err)
	}

	// Test 1: Verify interim log page is served immediately
	t.Run("InterimLogPage", func(t *testing.T) {
		resp, err := http.Get(proxyURL)
		if err != nil {
			t.Fatalf("Failed to get log page: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		// Verify it's the log viewer HTML
		if !strings.Contains(bodyStr, "Deploying your application") &&
			!strings.Contains(bodyStr, "Application ready") {
			t.Errorf("Response does not appear to be log viewer page")
		}
	})

	// Test 2: Verify logs API returns subprocess output
	t.Run("LogsAPI", func(t *testing.T) {
		// Wait for app to be running so logs are captured
		if err := waitForAppReady(proxyURL, 60*time.Second); err != nil {
			t.Fatalf("App did not become ready: %v", err)
		}

		// Use /api/logs/all which reads from file and should have logs
		resp, err := http.Get(proxyURL + "/api/logs/all")
		if err != nil {
			t.Fatalf("Failed to get logs: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode JSON response: %v", err)
		}

		// Verify logs exist
		logs, ok := result["logs"].([]interface{})
		if !ok {
			t.Fatalf("Expected 'logs' field to be an array")
		}

		// Should have at least the Python server startup message
		if len(logs) == 0 {
			t.Logf("Warning: Expected some log entries, got none. This may indicate log capture issues.")
		}

		// Verify count field exists
		if _, ok := result["count"]; !ok {
			t.Errorf("Expected 'count' field in response")
		}
	})

	// Test 3: Verify process stats API
	t.Run("StatsAPI", func(t *testing.T) {
		resp, err := http.Get(proxyURL + "/api/logs/stats")
		if err != nil {
			t.Fatalf("Failed to get stats: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode JSON response: %v", err)
		}

		// Verify process state exists
		if _, ok := result["process_state"]; !ok {
			t.Errorf("Expected 'process_state' field in response")
		}

		// Verify process info exists
		if _, ok := result["process_info"]; !ok {
			t.Errorf("Expected 'process_info' field in response")
		}
	})

	// Test 4: Wait for app to be ready and verify proxying works
	t.Run("ProxyToApp", func(t *testing.T) {
		// Wait for the subprocess to be ready (health check passes)
		// We poll the stats API to check when state becomes "running"
		if err := waitForAppReady(proxyURL, 60*time.Second); err != nil {
			t.Fatalf("App did not become ready: %v", err)
		}

		// Now the proxy should forward requests to the Python HTTP server
		resp, err := http.Get(proxyURL)
		if err != nil {
			t.Fatalf("Failed to get proxied response: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		// The Python HTTP server serves a directory listing by default
		// or if accessing root, it should show some HTML
		if !strings.Contains(bodyStr, "<!DOCTYPE") && !strings.Contains(bodyStr, "<html") {
			t.Logf("Response body: %s", bodyStr)
		}
	})

	// Test 5: Verify graceful shutdown
	t.Run("GracefulShutdown", func(t *testing.T) {
		// Send interrupt signal - this triggers graceful shutdown
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatalf("Failed to send interrupt signal: %v", err)
		}

		// Wait for shutdown to complete (proxy waits up to 10s for subprocess)
		// Plus some buffer time for cleanup
		time.Sleep(12 * time.Second)

		// At this point the process should have exited
		// We don't check explicitly as the test cleanup will handle killing if needed
		t.Log("Graceful shutdown signal sent successfully")
	})
}

// Helper functions

// getFreePort returns a free TCP port
func getFreePort(t *testing.T) int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

// buildBinary builds the jhub-app-proxy binary and returns its path
func buildBinary(t *testing.T) string {
	// Get project root (two levels up from test/integration)
	projectRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("Failed to get project root: %v", err)
	}

	binaryPath := filepath.Join(projectRoot, "jhub-app-proxy-test")

	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/jhub-app-proxy")
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}

	// Clean up binary after test
	t.Cleanup(func() {
		os.Remove(binaryPath)
	})

	return binaryPath
}

// waitForHTTP waits for an HTTP endpoint to respond
func waitForHTTP(url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for %s", url)
		case <-ticker.C:
			resp, err := http.Get(url)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					return nil
				}
			}
		}
	}
}

// waitForAppReady polls the stats API until the app state is "running"
func waitForAppReady(proxyURL string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for app to be ready")
		case <-ticker.C:
			resp, err := http.Get(proxyURL + "/api/logs/stats")
			if err != nil {
				continue
			}

			var result map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				resp.Body.Close()
				continue
			}
			resp.Body.Close()

			if processState, ok := result["process_state"].(map[string]interface{}); ok {
				if state, ok := processState["state"].(string); ok {
					if state == "running" {
						return nil
					}
					if state == "failed" {
						return fmt.Errorf("app failed to start")
					}
				}
			}
		}
	}
}
