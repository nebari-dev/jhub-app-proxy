// Package auth provides JupyterHub OAuth authentication
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
)

// OAuthMiddleware handles JupyterHub OAuth authentication
type OAuthMiddleware struct {
	clientID   string
	apiToken   string
	apiURL     string
	baseURL    string
	hubHost    string
	hubPrefix  string
	cookieName string
	logger     *logger.Logger
}

// NewOAuthMiddleware creates a new OAuth middleware
func NewOAuthMiddleware(log *logger.Logger) (*OAuthMiddleware, error) {
	apiURL := os.Getenv("JUPYTERHUB_API_URL")
	if apiURL == "" {
		return nil, fmt.Errorf("JUPYTERHUB_API_URL not set")
	}

	apiToken := os.Getenv("JUPYTERHUB_API_TOKEN")
	if apiToken == "" {
		return nil, fmt.Errorf("JUPYTERHUB_API_TOKEN not set")
	}

	clientID := os.Getenv("JUPYTERHUB_CLIENT_ID")
	if clientID == "" {
		clientID = os.Getenv("JUPYTERHUB_SERVICE_PREFIX")
	}

	baseURL := os.Getenv("JUPYTERHUB_SERVICE_PREFIX")
	if baseURL == "" {
		baseURL = "/"
	}
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	hubHost := os.Getenv("JUPYTERHUB_HOST")
	hubPrefix := os.Getenv("JUPYTERHUB_BASE_URL")
	if hubPrefix == "" {
		hubPrefix = "/hub/"
	}
	if !strings.HasSuffix(hubPrefix, "/") {
		hubPrefix += "/"
	}

	return &OAuthMiddleware{
		clientID:   clientID,
		apiToken:   apiToken,
		apiURL:     apiURL,
		baseURL:    baseURL,
		hubHost:    hubHost,
		hubPrefix:  hubPrefix,
		cookieName: clientID,
		logger:     log.WithComponent("oauth"),
	}, nil
}

// Wrap wraps an HTTP handler with OAuth authentication
func (m *OAuthMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle OAuth callback
		if strings.HasSuffix(r.URL.Path, "/oauth_callback") {
			m.handleCallback(w, r)
			return
		}

		// Check for token in cookie
		cookie, err := r.Cookie(m.cookieName)
		if err == nil && cookie.Value != "" {
			// Validate token (you could add caching here if needed)
			if m.validateToken(cookie.Value) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// No valid token, redirect to OAuth
		m.redirectToLogin(w, r)
	})
}

func (m *OAuthMiddleware) validateToken(token string) bool {
	req, _ := http.NewRequest("GET", m.apiURL+"/user", nil)
	req.Header.Set("Authorization", "token "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

func (m *OAuthMiddleware) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	// Generate random state
	b := make([]byte, 16)
	rand.Read(b)
	state := base64.URLEncoding.EncodeToString(b)

	// Set state cookie
	http.SetCookie(w, &http.Cookie{
		Name:     m.cookieName + "-oauth-state",
		Value:    state,
		Path:     m.baseURL,
		MaxAge:   600,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})

	// Build OAuth URL
	redirectURI := m.baseURL + "oauth_callback"
	authURL := fmt.Sprintf("%s%sapi/oauth2/authorize?client_id=%s&redirect_uri=%s&response_type=code&state=%s",
		m.hubHost, m.hubPrefix, url.QueryEscape(m.clientID), url.QueryEscape(redirectURI), url.QueryEscape(state))

	http.Redirect(w, r, authURL, http.StatusFound)
}

func (m *OAuthMiddleware) handleCallback(w http.ResponseWriter, r *http.Request) {
	// Get code and state
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" {
		http.Error(w, "No code provided", http.StatusBadRequest)
		return
	}

	// Validate state
	stateCookie, err := r.Cookie(m.cookieName + "-oauth-state")
	if err != nil || stateCookie.Value != state {
		http.Error(w, "Invalid state", http.StatusForbidden)
		return
	}

	// Exchange code for token
	redirectURI := m.baseURL + "oauth_callback"
	data := url.Values{}
	data.Set("client_id", m.clientID)
	data.Set("client_secret", m.apiToken)
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)

	req, _ := http.NewRequest("POST", m.apiURL+"/oauth2/token", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "Token exchange failed", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		m.logger.Error("token exchange failed", fmt.Errorf("status %d: %s", resp.StatusCode, string(body)))
		http.Error(w, "Token exchange failed", http.StatusInternalServerError)
		return
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		http.Error(w, "Failed to parse token", http.StatusInternalServerError)
		return
	}

	// Clear state cookie
	http.SetCookie(w, &http.Cookie{
		Name:   m.cookieName + "-oauth-state",
		Value:  "",
		Path:   m.baseURL,
		MaxAge: -1,
	})

	// Set token cookie
	http.SetCookie(w, &http.Cookie{
		Name:     m.cookieName,
		Value:    tokenResp.AccessToken,
		Path:     m.baseURL,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect to base URL
	http.Redirect(w, r, m.baseURL, http.StatusFound)
}
