// oauth_callback_test.go - Tests OAuth callback flow for interim pages
//
// This test reproduces the issue where OAuth callback fails with 403 Forbidden
// when trying to authenticate to interim pages/logs API.

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestOAuthCallbackForInterimPages tests the complete OAuth flow for interim pages
// This test currently FAILS due to the OAuth callback routing issue
func TestOAuthCallbackForInterimPages(t *testing.T) {
	// Get free ports for proxy, subprocess, and mock JupyterHub
	proxyPort := getFreePort(t)
	destPort := getFreePort(t)
	hubPort := getFreePort(t)

	// Start mock JupyterHub OAuth server
	hubURL := fmt.Sprintf("http://127.0.0.1:%d", hubPort)
	mockHub := startMockJupyterHub(t, hubPort)
	defer mockHub.Shutdown(context.Background())

	// Build the binary
	binaryPath := buildBinary(t)

	// Start jhub-app-proxy with OAuth authentication
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath,
		"--port", fmt.Sprintf("%d", proxyPort),
		"--destport", fmt.Sprintf("%d", destPort),
		"--authtype", "oauth",
		"--log-format", "pretty",
		"--log-level", "debug",
		"--",
		"python3", "-m", "http.server", "{port}",
	)

	// Set JupyterHub environment variables pointing to mock server
	cmd.Env = append(os.Environ(),
		"JUPYTERHUB_API_TOKEN=test-token-12345",
		"JUPYTERHUB_API_URL="+hubURL+"/hub/api",
		"JUPYTERHUB_HOST="+hubURL,
		"JUPYTERHUB_BASE_URL=/hub/",
		"JUPYTERHUB_USER=testuser",
		"JUPYTERHUB_SERVICE_PREFIX=/user/testuser/",
		"JUPYTERHUB_CLIENT_ID=jupyterhub-user-testuser",
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
	servicePrefix := "/user/testuser"
	interimPath := servicePrefix + "/_temp/jhub-app-proxy"

	// Wait for proxy to be ready (we expect it to redirect to OAuth, not return 200)
	time.Sleep(2 * time.Second)

	t.Run("OAuthFlowForLogsAPI", func(t *testing.T) {
		// Create HTTP client with cookie jar to maintain session
		jar, _ := cookiejar.New(nil)
		client := &http.Client{
			Jar: jar,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Allow following redirects but track them
				t.Logf("Redirect: %s -> %s", via[len(via)-1].URL.String(), req.URL.String())
				return nil
			},
			Timeout: 10 * time.Second,
		}

		// Step 1: Request interim logs API
		logsAPIURL := proxyURL + interimPath + "/api/logs/stats"
		t.Logf("Step 1: Requesting logs API at %s", logsAPIURL)

		resp, err := client.Get(logsAPIURL)
		if err != nil {
			t.Fatalf("Failed to request logs API: %v", err)
		}
		defer resp.Body.Close()

		// At this point, the OAuth flow should have completed
		// The mock JupyterHub automatically approves OAuth and redirects back
		t.Logf("Final response status: %d", resp.StatusCode)
		t.Logf("Final response URL: %s", resp.Request.URL.String())

		// Check if we ended up at the callback URL with 403
		if resp.StatusCode == 403 {
			t.Errorf("BUG REPRODUCED: OAuth callback returned 403 Forbidden")
			t.Logf("This indicates the state cookie was not found or didn't match")
			t.Logf("Root cause: Interim OAuth middleware sets state cookie, but proxy OAuth middleware handles callback")
		}

		// The test should pass when the fix is implemented
		if resp.StatusCode != 200 {
			t.Errorf("Expected 200 OK after OAuth flow, got %d", resp.StatusCode)
		}

		// Verify we can actually access the logs API after authentication
		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Errorf("Failed to decode logs API response: %v", err)
		} else {
			if _, ok := result["process_state"]; !ok {
				t.Errorf("Expected process_state in logs API response")
			}
		}
	})
}

// startMockJupyterHub starts a mock JupyterHub server for testing OAuth
func startMockJupyterHub(t *testing.T, port int) *http.Server {
	mux := http.NewServeMux()

	// Mock OAuth authorize endpoint - immediately redirect back with code
	mux.HandleFunc("/hub/api/oauth2/authorize", func(w http.ResponseWriter, r *http.Request) {
		clientID := r.URL.Query().Get("client_id")
		redirectURI := r.URL.Query().Get("redirect_uri")
		state := r.URL.Query().Get("state")

		t.Logf("Mock OAuth authorize: client_id=%s, redirect_uri=%s", clientID, redirectURI)

		// Auto-approve and redirect back with code
		// If redirect_uri is relative, make it absolute to jhub-app-proxy
		callbackURL := redirectURI
		if strings.HasPrefix(redirectURI, "/") {
			// Extract the Referer to know where the request came from
			referer := r.Referer()
			if referer == "" {
				// Fallback: construct from the request
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				callbackURL = fmt.Sprintf("%s://%s%s", scheme, r.Host, redirectURI)
			} else {
				// Parse referer to get the jhub-app-proxy URL
				if strings.Contains(referer, "://") {
					parts := strings.SplitN(referer, "/", 4)
					if len(parts) >= 3 {
						baseURL := parts[0] + "//" + parts[2]
						callbackURL = baseURL + redirectURI
					}
				}
			}
		}
		callbackURL += "?code=test-auth-code-12345&state=" + state
		t.Logf("Mock OAuth redirecting to: %s", callbackURL)
		http.Redirect(w, r, callbackURL, http.StatusFound)
	})

	// Mock OAuth token exchange endpoint
	mux.HandleFunc("/hub/api/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse form data
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		code := r.FormValue("code")
		t.Logf("Mock OAuth token exchange: code=%s", code)

		// Return access token
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-access-token-67890",
			"token_type":   "Bearer",
		})
	})

	// Mock user API endpoint for token validation
	mux.HandleFunc("/hub/api/user", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		t.Logf("Mock user API: Authorization=%s", auth)

		if !strings.HasPrefix(auth, "token ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Return user info
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name":   "testuser",
			"admin":  false,
			"groups": []string{},
			"scopes": []string{"access:servers"},
		})
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			t.Logf("Mock JupyterHub server error: %v", err)
		}
	}()

	// Wait for server to be ready
	time.Sleep(500 * time.Millisecond)
	t.Logf("Mock JupyterHub started on port %d", port)

	return server
}
