// security_interim_only_test.go - Tests --interim-page-auth flag
//
// Verifies the --interim-page-auth flag behavior:
// - Main application: accessible without authentication (200 OK)
// - Interim pages & logs API: requires authentication (302 redirect)

package integration

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestInterimPageAuthFlag tests the --interim-page-auth flag
// This flag allows protecting interim pages and logs API while keeping the main app public
func TestInterimPageAuthFlag(t *testing.T) {
	// Get free ports for proxy and subprocess
	proxyPort := getFreePort(t)
	destPort := getFreePort(t)

	// Build the binary first
	binaryPath := buildBinary(t)

	// Start jhub-app-proxy with --authtype=none but --interim-page-auth=true
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath,
		"--port", fmt.Sprintf("%d", proxyPort),
		"--destport", fmt.Sprintf("%d", destPort),
		"--authtype", "none", // Main app is PUBLIC
		"--interim-page-auth", // But interim pages are PROTECTED
		"--log-format", "pretty",
		"--log-level", "info",
		"--",
		"python3", "-m", "http.server", "{port}",
	)

	// Set minimal JupyterHub environment variables (required for OAuth)
	cmd.Env = append(os.Environ(),
		"JUPYTERHUB_API_TOKEN=test-token-12345",
		"JUPYTERHUB_API_URL=http://localhost:8081/hub/api",
		"JUPYTERHUB_USER=testuser",
		"JUPYTERHUB_SERVICE_PREFIX=/user/testuser/",
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start jhub-app-proxy: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			if err := cmd.Process.Kill(); err != nil {
				t.Logf("Failed to kill process: %v", err)
			}
		}
	}()

	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", proxyPort)
	servicePrefix := "/user/testuser"
	interimPath := servicePrefix + "/_temp/jhub-app-proxy"

	// Wait for proxy to be ready (use main app since interim is protected)
	if err := waitForHTTP(proxyURL+servicePrefix+"/", 5*time.Second); err != nil {
		t.Fatalf("Proxy did not become ready: %v", err)
	}

	// Give the subprocess time to fully start (we can't use stats API since it's protected)
	time.Sleep(3 * time.Second)

	// Test 1: Main app should be PUBLIC (no auth required)
	t.Run("MainAppIsPublic", func(t *testing.T) {
		resp, err := http.Get(proxyURL + servicePrefix + "/")
		if err != nil {
			t.Fatalf("Failed to request main app: %v", err)
		}
		defer resp.Body.Close()

		// Should return 200 OK - app is public!
		if resp.StatusCode != 200 {
			t.Errorf("Expected 200 for public app, got %d", resp.StatusCode)
		}
	})

	// Test 2: Interim page should be PROTECTED (auth required)
	t.Run("InterimPageIsProtected", func(t *testing.T) {
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		resp, err := client.Get(proxyURL + interimPath)
		if err != nil {
			t.Fatalf("Failed to request interim page: %v", err)
		}
		defer resp.Body.Close()

		// Should NOT return 200 - interim page is protected!
		if resp.StatusCode == 200 {
			t.Errorf("SECURITY ISSUE: Interim page should be protected but got 200")
		}

		// Should redirect to OAuth
		if resp.StatusCode != 302 {
			t.Errorf("Expected 302 redirect for protected interim page, got %d", resp.StatusCode)
		}
	})

	// Test 3: Logs API should be PROTECTED (auth required)
	t.Run("LogsAPIIsProtected", func(t *testing.T) {
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		resp, err := client.Get(proxyURL + interimPath + "/api/logs/all")
		if err != nil {
			t.Fatalf("Failed to request logs API: %v", err)
		}
		defer resp.Body.Close()

		// Should NOT return 200 - logs API is protected!
		if resp.StatusCode == 200 {
			t.Errorf("SECURITY ISSUE: Logs API should be protected but got 200")
		}

		// Should redirect to OAuth
		if resp.StatusCode != 302 {
			t.Errorf("Expected 302 redirect for protected logs API, got %d", resp.StatusCode)
		}
	})
}
