// jupyterhub_oauth_test.go - Integration tests with real JupyterHub using testcontainers
//
// Tests OAuth authentication flow with a real JupyterHub instance running in Docker.

package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestOAuthWithJupyterHub tests OAuth authentication with a real JupyterHub instance
func TestOAuthWithJupyterHub(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test with Docker in short mode")
	}

	ctx := context.Background()

	jupyterhubContainer, hubAPIURL, hubURL, err := startJupyterHubContainer(ctx, t)
	if err != nil {
		t.Fatalf("Failed to start JupyterHub: %v", err)
	}
	defer func() {
		if err := jupyterhubContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	}()

	// Wait for JupyterHub API to be ready
	if err := waitForJupyterHubAPI(hubAPIURL, 30*time.Second); err != nil {
		t.Fatalf("JupyterHub API not ready: %v", err)
	}

	// Get free ports for proxy and subprocess
	proxyPort := getFreePort(t)
	destPort := getFreePort(t)

	// Build the binary
	binaryPath := buildBinary(t)

	// Start jhub-app-proxy with OAuth authentication
	testCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(testCtx, binaryPath,
		"--port", fmt.Sprintf("%d", proxyPort),
		"--destport", fmt.Sprintf("%d", destPort),
		"--authtype", "oauth",
		"--log-format", "pretty",
		"--log-level", "debug",
		"--",
		"python3", "-m", "http.server", "{port}",
	)

	// Set JupyterHub environment variables
	cmd.Env = append(os.Environ(),
		"JUPYTERHUB_API_TOKEN=test-token-12345",
		fmt.Sprintf("JUPYTERHUB_API_URL=%s", hubAPIURL),
		"JUPYTERHUB_USER=testuser",
		"JUPYTERHUB_SERVICE_PREFIX=/user/testuser/",
		"JUPYTERHUB_CLIENT_ID=service-test-service",
		fmt.Sprintf("JUPYTERHUB_HOST=%s", hubURL),
		"JUPYTERHUB_BASE_URL=/",
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

	// Wait for proxy to be ready
	if err := waitForHTTP(proxyURL+servicePrefix+"/", 10*time.Second); err != nil {
		t.Fatalf("Proxy did not become ready: %v", err)
	}

	// Test 1: Unauthenticated request to interim page should redirect to OAuth
	t.Run("InterimPageRequiresAuth", func(t *testing.T) {
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse // Don't follow redirects
			},
		}

		// Access the interim page directly (not the root during startup)
		interimPagePath := servicePrefix + "/_temp/jhub-app-proxy"
		resp, err := client.Get(proxyURL + interimPagePath)
		if err != nil {
			t.Fatalf("Failed to request interim page: %v", err)
		}
		defer resp.Body.Close()

		// Should redirect to OAuth (302)
		if resp.StatusCode != http.StatusFound {
			t.Errorf("Expected 302 redirect for interim page, got %d", resp.StatusCode)
		}

		// Check the Location header points to JupyterHub OAuth
		location := resp.Header.Get("Location")
		if !strings.Contains(location, "/hub/api/oauth2/authorize") {
			t.Errorf("Expected redirect to OAuth endpoint, got: %s", location)
		}

		t.Logf("Redirect location: %s", location)
	})

	// Test 2: Request with invalid token should be rejected
	t.Run("InvalidTokenRejected", func(t *testing.T) {
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse // Don't follow redirects
			},
		}

		// Try with an invalid token
		req, err := http.NewRequest("GET", proxyURL+servicePrefix+"/_temp/jhub-app-proxy", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("X-Jupyterhub-Api-Token", "invalid-token-999")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to execute request: %v", err)
		}
		defer resp.Body.Close()

		// Invalid token should redirect to OAuth login
		if resp.StatusCode != http.StatusFound {
			t.Errorf("Expected 302 redirect for invalid token, got %d", resp.StatusCode)
		}

		location := resp.Header.Get("Location")
		if !strings.Contains(location, "/hub/api/oauth2/authorize") {
			t.Errorf("Expected redirect to OAuth, got: %s", location)
		}
	})

	// Test 3: Request with valid service token should be allowed
	t.Run("ValidServiceTokenAllowsAccess", func(t *testing.T) {
		// First verify the token works directly with JupyterHub
		req, err := http.NewRequest("GET", hubAPIURL+"/user", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "token test-token-12345")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to call JupyterHub API: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			t.Logf("JupyterHub API response: %s", string(body))
			t.Fatalf("JupyterHub didn't accept the token. Status: %d", resp.StatusCode)
		}

		t.Log("✓ Token validated successfully by JupyterHub")

		// Now test that jhub-app-proxy accepts this token
		req, err = http.NewRequest("GET", proxyURL+servicePrefix+"/_temp/jhub-app-proxy", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		// Add the API token that matches the service configuration
		req.Header.Set("X-Jupyterhub-Api-Token", "test-token-12345")

		client := &http.Client{}
		resp, err = client.Do(req)
		if err != nil {
			t.Fatalf("Failed to execute request: %v", err)
		}
		defer resp.Body.Close()

		// With valid token validated by JupyterHub, we should get through
		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			t.Logf("Response status: %d", resp.StatusCode)
			t.Logf("Response body: %s", string(body))
			t.Errorf("Expected 200 with valid token, got %d", resp.StatusCode)
		}

		t.Log("✓ jhub-app-proxy accepted the validated token")
	})

	// Test 4: OAuth callback endpoint should exist
	t.Run("OAuthCallbackExists", func(t *testing.T) {
		// Try to access the callback endpoint (without code, it should fail but endpoint exists)
		resp, err := http.Get(proxyURL + servicePrefix + "/oauth_callback")
		if err != nil {
			t.Fatalf("Failed to request callback: %v", err)
		}
		defer resp.Body.Close()

		// Should return 400 (bad request - no code) or 403 (invalid state)
		// but NOT 404 (endpoint exists)
		if resp.StatusCode == 404 {
			t.Errorf("OAuth callback endpoint not found")
		}

		t.Logf("Callback endpoint status: %d", resp.StatusCode)
	})
}
