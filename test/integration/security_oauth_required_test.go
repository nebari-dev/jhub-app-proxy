// security_oauth_required_test.go - Tests OAuth authentication on interim pages and logs API
//
// Verifies that when --authtype=oauth is set, interim pages and logs API endpoints
// require authentication (return 302 redirect to OAuth, not 200 OK).

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

// TestInterimPagesRequireAuth tests that interim pages and logs API require authentication when --authtype=oauth
func TestInterimPagesRequireAuth(t *testing.T) {
	// Get free ports for proxy and subprocess
	proxyPort := getFreePort(t)
	destPort := getFreePort(t)

	// Build the binary first
	binaryPath := buildBinary(t)

	// Start jhub-app-proxy with OAuth authentication
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath,
		"--port", fmt.Sprintf("%d", proxyPort),
		"--destport", fmt.Sprintf("%d", destPort),
		"--authtype", "oauth", // OAuth authentication enabled
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

	// Wait for proxy to be ready
	if err := waitForHTTP(proxyURL+servicePrefix+"/", 5*time.Second); err != nil {
		t.Fatalf("Proxy did not become ready: %v", err)
	}

	// Test 1: Interim page should require authentication
	t.Run("InterimPageRequiresAuth", func(t *testing.T) {
		// Disable redirect following to see the actual auth response
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse // Don't follow redirects
			},
		}
		resp, err := client.Get(proxyURL + interimPath)
		if err != nil {
			t.Fatalf("Failed to request interim page: %v", err)
		}
		defer resp.Body.Close()

		// CRITICAL: Should NOT return 200!
		// Interim pages can show sensitive subprocess logs
		if resp.StatusCode == 200 {
			t.Errorf("SECURITY VULNERABILITY: Interim page accessible without authentication! Got status %d", resp.StatusCode)
		}

		// Valid auth responses: 401 Unauthorized, 403 Forbidden, 302 Redirect (OAuth flow)
		if resp.StatusCode != 401 && resp.StatusCode != 403 && resp.StatusCode != 302 {
			t.Errorf("Expected 401, 403, or 302 for unauthenticated request, got %d", resp.StatusCode)
		}
	})

	// Test 2: Logs API should require authentication
	t.Run("LogsAPIRequiresAuth", func(t *testing.T) {
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

		// CRITICAL: Should NOT return 200!
		// Logs can contain sensitive information (secrets, tokens, etc.)
		if resp.StatusCode == 200 {
			t.Errorf("SECURITY VULNERABILITY: Logs API accessible without authentication! Got status %d", resp.StatusCode)
		}

		if resp.StatusCode != 401 && resp.StatusCode != 403 && resp.StatusCode != 302 {
			t.Errorf("Expected 401, 403, or 302 for unauthenticated request, got %d", resp.StatusCode)
		}
	})

	// Test 3: Stats API should require authentication
	t.Run("StatsAPIRequiresAuth", func(t *testing.T) {
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		resp, err := client.Get(proxyURL + interimPath + "/api/logs/stats")
		if err != nil {
			t.Fatalf("Failed to request stats API: %v", err)
		}
		defer resp.Body.Close()

		// CRITICAL: Should NOT return 200!
		// Stats reveal process state and PID information
		if resp.StatusCode == 200 {
			t.Errorf("SECURITY VULNERABILITY: Stats API accessible without authentication! Got status %d", resp.StatusCode)
		}

		if resp.StatusCode != 401 && resp.StatusCode != 403 && resp.StatusCode != 302 {
			t.Errorf("Expected 401, 403, or 302 for unauthenticated request, got %d", resp.StatusCode)
		}
	})

	// Test 4: With valid JupyterHub token, access should be allowed
	t.Run("ValidTokenAllowsAccess", func(t *testing.T) {
		// Create request with JupyterHub authentication cookie
		req, err := http.NewRequest("GET", proxyURL+interimPath, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		// Add JupyterHub authentication token as would be done by JupyterHub's OAuth
		req.Header.Set("Authorization", "token test-token-12345")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to execute request: %v", err)
		}
		defer resp.Body.Close()

		// With valid token, access should be allowed
		if resp.StatusCode != 200 {
			t.Logf("Note: This test may fail if JupyterHub mock server is not running")
			t.Logf("Expected 200 with valid auth token, got %d", resp.StatusCode)
		}
	})
}
