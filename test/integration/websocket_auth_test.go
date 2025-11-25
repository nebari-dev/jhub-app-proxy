// websocket_auth_test.go - Tests WebSocket authentication
//
// Verifies that WebSocket upgrade requests require authentication when --authtype=oauth is set.
// This test replicates jupyter-server-proxy's test_websocket_no_auth_failure to ensure
// we don't have the same vulnerability (GHSA-w3vc-fx9p-wp4v).

package integration

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// startProxyWithOAuth starts jhub-app-proxy with OAuth and waits for backend to be ready
func startProxyWithOAuth(t *testing.T, hubAPIURL string) (proxyURL, servicePrefix string, cleanup func()) {
	proxyPort := getFreePort(t)
	destPort := getFreePort(t)
	binaryPath := buildBinary(t)

	// Build and get path to WebSocket echo server
	wsEchoPath := buildWebSocketEchoServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	cmd := exec.CommandContext(ctx, binaryPath,
		"--port", fmt.Sprintf("%d", proxyPort),
		"--destport", fmt.Sprintf("%d", destPort),
		"--authtype", "oauth",
		"--log-format", "pretty",
		"--log-level", "info",
		"--",
		wsEchoPath, "-port", "{port}",
	)

	cmd.Env = append(os.Environ(),
		"JUPYTERHUB_API_TOKEN=test-token-12345",
		fmt.Sprintf("JUPYTERHUB_API_URL=%s", hubAPIURL),
		"JUPYTERHUB_USER=testuser",
		"JUPYTERHUB_SERVICE_PREFIX=/user/testuser/",
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start jhub-app-proxy: %v", err)
	}

	proxyURL = fmt.Sprintf("http://127.0.0.1:%d", proxyPort)
	servicePrefix = "/user/testuser"

	// Wait for proxy to be ready
	if err := waitForHTTP(proxyURL+servicePrefix+"/", 10*time.Second); err != nil {
		cancel()
		cmd.Process.Kill()
		t.Fatalf("Proxy did not become ready: %v", err)
	}

	// Wait for WebSocket backend to be fully running (grace period is 10s)
	// We need to wait for the health check to pass and the app to be marked as running
	time.Sleep(12 * time.Second)

	cleanup = func() {
		cancel()
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}

	return proxyURL, servicePrefix, cleanup
}

// TestWebSocketUpgradeRequiresAuth verifies that WebSocket upgrade requests without authentication
// are properly rejected with OAuth redirect (302), protecting against the authentication bypass
// vulnerability (GHSA-w3vc-fx9p-wp4v) found in jupyter-server-proxy.
//
// Tests two attack vectors:
//  1. WebSocket client (websocket.Dial) without auth headers
//  2. Raw HTTP request with WebSocket Upgrade headers but no auth
//
// Both must result in OAuth redirect, NOT 101 Switching Protocols.
func TestWebSocketUpgradeRequiresAuth(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test with Docker in short mode")
	}

	ctx := context.Background()
	jupyterhubContainer, hubAPIURL, _, err := startJupyterHubContainer(ctx, t)
	if err != nil {
		t.Fatalf("Failed to start JupyterHub: %v", err)
	}
	defer jupyterhubContainer.Terminate(ctx)

	if err := waitForJupyterHubAPI(hubAPIURL, 30*time.Second); err != nil {
		t.Fatalf("JupyterHub API not ready: %v", err)
	}

	proxyURL, servicePrefix, cleanup := startProxyWithOAuth(t, hubAPIURL)
	defer cleanup()

	t.Run("WebSocketDialWithoutAuth", func(t *testing.T) {
		wsURL := fmt.Sprintf("ws://%s%s/", strings.TrimPrefix(proxyURL, "http://"), servicePrefix)

		dialer := websocket.Dialer{
			HandshakeTimeout: 2 * time.Second,
		}

		conn, resp, err := dialer.Dial(wsURL, nil)

		// CRITICAL: If connection was established, this is a security failure
		if conn != nil {
			conn.Close()
			t.Fatalf("SECURITY FAILURE: WebSocket connection established without authentication!")
		}

		t.Logf("WebSocket dial error: %v", err)
		if resp != nil {
			t.Logf("Response status: %d %s", resp.StatusCode, resp.Status)
			if location := resp.Header.Get("Location"); location != "" {
				t.Logf("Redirect location: %s", location)
			}
		}

		// Check: The WebSocket upgrade should be REJECTED (must NOT get 101)
		if resp != nil && resp.StatusCode == http.StatusSwitchingProtocols {
			t.Fatalf("SECURITY FAILURE: WebSocket upgrade succeeded (got 101 Switching Protocols)")
		}

		if resp == nil {
			t.Logf("WebSocket connection rejected (no HTTP response)")
			return
		}

		// Verify OAuth redirect
		if resp.StatusCode != 302 {
			t.Errorf("Expected 302 (OAuth redirect), got %d", resp.StatusCode)
		}

		location := resp.Header.Get("Location")
		if !strings.Contains(location, "/oauth2/authorize") {
			t.Errorf("Expected OAuth redirect, got Location: %s", location)
		}
	})

	t.Run("HTTPClientWithUpgradeHeaders", func(t *testing.T) {
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}

		req, err := http.NewRequest("GET", proxyURL+servicePrefix+"/", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		// Set WebSocket upgrade headers manually to test that OAuth is enforced
		// even when all WebSocket headers are present
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-WebSocket-Version", "13")
		// Sec-WebSocket-Key: base64-encoded 16-byte nonce required by RFC 6455
		// Using the standard example value from the WebSocket spec
		req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		t.Logf("HTTP request with Upgrade headers: %d %s", resp.StatusCode, resp.Status)
		if location := resp.Header.Get("Location"); location != "" {
			t.Logf("Redirect location: %s", location)
		}

		if resp.StatusCode == http.StatusSwitchingProtocols {
			t.Fatalf("SECURITY FAILURE: WebSocket upgrade succeeded (got 101 Switching Protocols)")
		}

		if resp.StatusCode != 302 {
			t.Errorf("Expected 302 (OAuth redirect), got %d", resp.StatusCode)
		}

		location := resp.Header.Get("Location")
		if !strings.Contains(location, "/oauth2/authorize") {
			t.Errorf("Expected OAuth redirect, got Location: %s", location)
		}
	})
}

// TestWebSocketUpgradeWithValidAuth verifies that WebSocket connections with valid OAuth tokens
// are successfully upgraded (101 Switching Protocols) and function correctly.
//
// This test validates:
//  1. Valid JupyterHub API token allows WebSocket upgrade
//  2. Connection upgrade returns 101 status (not 302 redirect)
//  3. Bidirectional communication works (echo test)
//  4. The Hijacker interface implementation enables protocol upgrade
func TestWebSocketUpgradeWithValidAuth(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test with Docker in short mode")
	}

	ctx := context.Background()
	jupyterhubContainer, hubAPIURL, _, err := startJupyterHubContainer(ctx, t)
	if err != nil {
		t.Fatalf("Failed to start JupyterHub: %v", err)
	}
	defer jupyterhubContainer.Terminate(ctx)

	if err := waitForJupyterHubAPI(hubAPIURL, 30*time.Second); err != nil {
		t.Fatalf("JupyterHub API not ready: %v", err)
	}

	proxyURL, servicePrefix, cleanup := startProxyWithOAuth(t, hubAPIURL)
	defer cleanup()

	t.Run("WebSocketWithValidToken", func(t *testing.T) {
		wsURL := fmt.Sprintf("ws://%s%s/", strings.TrimPrefix(proxyURL, "http://"), servicePrefix)

		headers := http.Header{}
		headers.Set("X-Jupyterhub-Api-Token", "test-token-12345")

		dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
		conn, resp, err := dialer.Dial(wsURL, headers)

		t.Logf("WebSocket dial with valid token: err=%v", err)
		if resp != nil {
			t.Logf("Response status: %d %s", resp.StatusCode, resp.Status)
		}

		// With valid auth, should NOT get OAuth redirects (the key security test)
		if resp != nil {
			if resp.StatusCode == 302 && strings.Contains(resp.Header.Get("Location"), "oauth") {
				t.Fatalf("Got OAuth redirect even with valid token!")
			}
			if resp.StatusCode == 401 || resp.StatusCode == 403 {
				t.Fatalf("Got auth error %d even with valid token", resp.StatusCode)
			}
		}

		if conn == nil {
			t.Fatalf("WebSocket upgrade failed with valid auth token: err=%v, resp=%v", err, resp)
		}
		defer conn.Close()

		// If we get here, WebSocket upgrade succeeded - test the connection
		// Verify we got 101 Switching Protocols
		if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("Expected 101 Switching Protocols, got %d", resp.StatusCode)
		}

		// Test the WebSocket connection actually works
		testMessage := []byte("test echo")
		if err := conn.WriteMessage(websocket.TextMessage, testMessage); err != nil {
			t.Fatalf("Failed to send message: %v", err)
		}

		_, receivedMessage, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("Failed to receive message: %v", err)
		}

		if string(receivedMessage) != string(testMessage) {
			t.Errorf("Expected echo '%s', got '%s'", testMessage, receivedMessage)
		}

		t.Log("WebSocket upgrade succeeded and connection works with valid token")
	})
}
