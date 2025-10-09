// Package hub provides JupyterHub API client for activity reporting and authentication
package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
)

// Client is a JupyterHub API client
type Client struct {
	baseURL    string
	apiToken   string
	username   string
	servername string
	logger     *logger.Logger
	httpClient *http.Client
}

// Config holds JupyterHub client configuration
type Config struct {
	BaseURL    string // JupyterHub base URL (from JUPYTERHUB_BASE_URL or JUPYTERHUB_API_URL)
	APIToken   string // API token (from JUPYTERHUB_API_TOKEN)
	Username   string // Username (from JUPYTERHUB_USER)
	ServerName string // Server name (from JUPYTERHUB_SERVER_NAME or empty for default)
}

// NewClientFromEnv creates a Hub client from environment variables
// This is the typical way to initialize in a spawned process
func NewClientFromEnv(log *logger.Logger) (*Client, error) {
	cfg := Config{
		BaseURL:    os.Getenv("JUPYTERHUB_API_URL"),
		APIToken:   os.Getenv("JUPYTERHUB_API_TOKEN"),
		Username:   os.Getenv("JUPYTERHUB_USER"),
		ServerName: os.Getenv("JUPYTERHUB_SERVER_NAME"),
	}

	// Fallback to base URL if API URL not set
	if cfg.BaseURL == "" {
		cfg.BaseURL = os.Getenv("JUPYTERHUB_BASE_URL")
		if cfg.BaseURL != "" {
			cfg.BaseURL = cfg.BaseURL + "/hub/api"
		}
	}

	return NewClient(cfg, log)
}

// NewClient creates a new JupyterHub API client
func NewClient(cfg Config, log *logger.Logger) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("JUPYTERHUB_API_URL or JUPYTERHUB_BASE_URL must be set")
	}
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("JUPYTERHUB_API_TOKEN must be set")
	}
	if cfg.Username == "" {
		return nil, fmt.Errorf("JUPYTERHUB_USER must be set")
	}

	return &Client{
		baseURL:    cfg.BaseURL,
		apiToken:   cfg.APIToken,
		username:   cfg.Username,
		servername: cfg.ServerName,
		logger:     log.WithComponent("hub-client"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

// ActivityPayload represents the activity notification payload
type ActivityPayload struct {
	Servers      map[string]ServerActivity `json:"servers,omitempty"`
	LastActivity time.Time                 `json:"last_activity"`
}

// ServerActivity represents activity for a specific server
type ServerActivity struct {
	LastActivity time.Time `json:"last_activity"`
}

// NotifyActivity notifies JupyterHub of recent activity to prevent idle culling
// This is critical for keeping the spawned app alive
func (c *Client) NotifyActivity(ctx context.Context) error {
	endpoint := fmt.Sprintf("%s/users/%s/activity", c.baseURL, c.username)

	now := time.Now().UTC()
	payload := ActivityPayload{
		LastActivity: now,
	}

	// Include server-specific activity if server name is set
	if c.servername != "" {
		payload.Servers = map[string]ServerActivity{
			c.servername: {
				LastActivity: now,
			},
		}
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal activity payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", c.apiToken))
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	duration := time.Since(start)

	if err != nil {
		c.logger.HubAPICall("POST", endpoint, 0, duration, err)
		return fmt.Errorf("failed to notify activity: %w", err)
	}
	defer resp.Body.Close()

	c.logger.HubAPICall("POST", endpoint, resp.StatusCode, duration, nil)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("activity notification failed with status %d: %s",
			resp.StatusCode, string(body))
	}

	c.logger.Debug("activity notification successful")
	return nil
}

// StartActivityReporter starts a background goroutine that periodically reports activity
// Returns a cancel function to stop the reporter
func (c *Client) StartActivityReporter(ctx context.Context, interval time.Duration, forceAlive bool) context.CancelFunc {
	ctx, cancel := context.WithCancel(ctx)

	go func() {
		c.logger.Info("starting activity reporter",
			"interval", interval,
			"force_alive", forceAlive,
			"username", c.username,
			"servername", c.servername)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Report activity immediately on start if force_alive is enabled
		if forceAlive {
			if err := c.NotifyActivity(ctx); err != nil {
				c.logger.Error("failed to notify activity on start", err)
			}
		}

		for {
			select {
			case <-ctx.Done():
				c.logger.Info("activity reporter stopped")
				return
			case <-ticker.C:
				// In force_alive mode, always report activity
				// In normal mode, only report if there was actual activity
				// (for now, we always report - activity tracking can be added later)
				if err := c.NotifyActivity(ctx); err != nil {
					c.logger.Error("failed to notify activity", err,
						"username", c.username,
						"servername", c.servername)
				}
			}
		}
	}()

	return cancel
}

// GetUser retrieves user information from JupyterHub
func (c *Client) GetUser(ctx context.Context) (map[string]interface{}, error) {
	endpoint := fmt.Sprintf("%s/users/%s", c.baseURL, c.username)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", c.apiToken))

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	duration := time.Since(start)

	if err != nil {
		c.logger.HubAPICall("GET", endpoint, 0, duration, err)
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	defer resp.Body.Close()

	c.logger.HubAPICall("GET", endpoint, resp.StatusCode, duration, nil)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get user failed with status %d: %s",
			resp.StatusCode, string(body))
	}

	var user map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user response: %w", err)
	}

	return user, nil
}

// Ping checks if the JupyterHub API is reachable
func (c *Client) Ping(ctx context.Context) error {
	endpoint := fmt.Sprintf("%s/", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", c.apiToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to ping hub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ping failed with status %d", resp.StatusCode)
	}

	c.logger.Debug("hub ping successful")
	return nil
}
