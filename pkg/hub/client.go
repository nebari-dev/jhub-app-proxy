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

	"github.com/nebari-dev/jhub-app-proxy/pkg/activity"
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

// NotifyActivityWithTime notifies JupyterHub of activity with a specific timestamp
// This is used when keepAlive=false to report actual last activity time
func (c *Client) NotifyActivityWithTime(ctx context.Context, timestamp time.Time) error {
	endpoint := fmt.Sprintf("%s/users/%s/activity", c.baseURL, c.username)

	payload := ActivityPayload{
		LastActivity: timestamp,
	}

	// Include server-specific activity if server name is set
	if c.servername != "" {
		payload.Servers = map[string]ServerActivity{
			c.servername: {
				LastActivity: timestamp,
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

	c.logger.Debug("activity notification successful", "timestamp", timestamp)
	return nil
}

// StartActivityReporter starts a background goroutine that periodically reports activity
// Returns a cancel function to stop the reporter
//
// If keepAlive is true: Always report current time (prevent idle culling)
// If keepAlive is false: Only report when there's actual activity tracked by activityTracker
func (c *Client) StartActivityReporter(ctx context.Context, interval time.Duration, keepAlive bool, activityTracker *activity.Tracker) context.CancelFunc {
	ctx, cancel := context.WithCancel(ctx)

	go func() {
		c.logger.Info("starting activity reporter",
			"interval", interval,
			"keep_alive", keepAlive,
			"username", c.username,
			"servername", c.servername)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Report activity immediately on start if keepAlive is enabled
		if keepAlive {
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
				if keepAlive {
					// Always report current time (keep alive forever)
					if err := c.NotifyActivity(ctx); err != nil {
						c.logger.Error("failed to notify activity", err,
							"username", c.username,
							"servername", c.servername)
					}
				} else {
					// Only report if there was actual activity
					lastActivity := activityTracker.GetLastActivity()
					if lastActivity != nil {
						if err := c.NotifyActivityWithTime(ctx, *lastActivity); err != nil {
							c.logger.Error("failed to notify activity", err,
								"username", c.username,
								"servername", c.servername,
								"last_activity", lastActivity)
						}
					} else {
						// No activity yet, don't send notification
						c.logger.Debug("no activity to report yet")
					}
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
