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
	clientID     string
	apiToken     string
	apiURL       string
	baseURL      string
	hubHost      string
	hubPrefix    string
	cookieName   string
	headerName   string
	callbackPath string // Custom callback path (e.g., "oauth_callback" or "_temp/jhub-app-proxy/oauth_callback")
	logger       *logger.Logger
}

// NewOAuthMiddleware creates a new OAuth middleware with default callback path
func NewOAuthMiddleware(log *logger.Logger) (*OAuthMiddleware, error) {
	return NewOAuthMiddlewareWithCallbackPath(log, "oauth_callback")
}

// NewOAuthMiddlewareWithCallbackPath creates a new OAuth middleware with a custom callback path
func NewOAuthMiddlewareWithCallbackPath(log *logger.Logger, callbackPath string) (*OAuthMiddleware, error) {
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

	// JUPYTERHUB_BASE_URL is the base URL of the deployment (e.g., "/" or "/jupyter/")
	// NOT the Hub's base URL. JupyterHub strips "/hub" from the Hub's base_url
	// when setting this env var. We need to append "hub/" to get the Hub's base path,
	// just like JupyterHub's HubOAuth class does.
	deploymentBase := os.Getenv("JUPYTERHUB_BASE_URL")
	if deploymentBase == "" {
		deploymentBase = "/"
	}
	if !strings.HasSuffix(deploymentBase, "/") {
		deploymentBase += "/"
	}

	// Construct the Hub's base path by appending "hub/" to the deployment base
	hubPrefix := deploymentBase + "hub/"

	return &OAuthMiddleware{
		clientID:     clientID,
		apiToken:     apiToken,
		apiURL:       apiURL,
		baseURL:      baseURL,
		hubHost:      hubHost,
		hubPrefix:    hubPrefix,
		cookieName:   clientID,
		headerName:   "X-Jupyterhub-Api-Token",
		callbackPath: callbackPath,
		logger:       log.WithComponent("oauth"),
	}, nil
}

// Wrap wraps an HTTP handler with OAuth authentication
func (m *OAuthMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle OAuth callback
		// Check if the path ends with the callback path (e.g., "/oauth_callback" or "/_temp/jhub-app-proxy/oauth_callback")
		if strings.HasSuffix(r.URL.Path, "/"+m.callbackPath) {
			m.handleCallback(w, r)
			return
		}

		maybeProxy := func(token string) bool {
			if token == "" {
				return false
			}

			user, err := m.getUser(token)
			if err != nil {
				return false
			}

			pr := new(http.Request)
			*pr = *r

			userData, _ := json.Marshal(user)
			pr.Header.Set("X-Forwarded-User-Data", string(userData))

			m.logger.Info("setting user data in headers",
				"header", "X-Forwarded-User-Data",
				"user_name", user.Name,
				"user_admin", user.Admin,
				"user_roles", user.Roles,
				"user_groups", user.Groups,
				"user_scopes", user.Scopes,
				"user_data_json", string(userData))

			next.ServeHTTP(w, pr)
			return true
		}

		if maybeProxy(r.Header.Get(m.headerName)) {
			return
		}

		cookie, err := r.Cookie(m.cookieName)
		if err == nil && maybeProxy(cookie.Value) {
			return
		}

		// No valid token, redirect to OAuth
		m.redirectToLogin(w, r)
	})
}

type User struct {
	Name   string   `json:"name"`
	Admin  bool     `json:"admin"`
	Roles  []string `json:"roles"`
	Groups []string `json:"groups"`
	Scopes []string `json:"scopes"`
}

func (m *OAuthMiddleware) getUser(token string) (*User, error) {
	req, err := http.NewRequest("GET", m.apiURL+"/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request to %s returned status %d", req.URL.String(), resp.StatusCode)
	}

	var u User
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, err
	}

	return &u, nil
}

func (m *OAuthMiddleware) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	// Generate random state
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		m.logger.Error("failed to generate random state", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	state := base64.URLEncoding.EncodeToString(b)

	// Store original URL to redirect back after OAuth
	originalURL := r.URL.RequestURI()

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

	// Set original URL cookie to redirect back after OAuth
	http.SetCookie(w, &http.Cookie{
		Name:     m.cookieName + "-oauth-next",
		Value:    originalURL,
		Path:     m.baseURL,
		MaxAge:   600,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})

	// Build OAuth URL with custom callback path
	redirectURI := m.baseURL + m.callbackPath
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
	redirectURI := m.baseURL + m.callbackPath
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

	// Redirect back to original URL if saved, otherwise to base URL
	redirectURL := m.baseURL
	if nextCookie, err := r.Cookie(m.cookieName + "-oauth-next"); err == nil && nextCookie.Value != "" {
		redirectURL = nextCookie.Value
		// Clear the next URL cookie
		http.SetCookie(w, &http.Cookie{
			Name:   m.cookieName + "-oauth-next",
			Value:  "",
			Path:   m.baseURL,
			MaxAge: -1,
		})
	}

	http.Redirect(w, r, redirectURL, http.StatusFound)
}
