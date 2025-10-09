// Package health provides health checking functionality for spawned processes
package health

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
)

// CheckConfig holds configuration for health checking
type CheckConfig struct {
	URL              string        // URL to check (e.g., http://localhost:8501/health)
	Timeout          time.Duration // Overall timeout for ready state
	Interval         time.Duration // Interval between checks
	InitialDelay     time.Duration // Delay before first check
	SuccessThreshold int           // Number of consecutive successes required
	HTTPTimeout      time.Duration // Timeout for individual HTTP requests
}

// DefaultCheckConfig returns sensible defaults for health checking
func DefaultCheckConfig(url string) CheckConfig {
	return CheckConfig{
		URL:              url,
		Timeout:          5 * time.Minute,
		Interval:         1 * time.Second,
		InitialDelay:     2 * time.Second,
		SuccessThreshold: 1,
		HTTPTimeout:      2 * time.Second,
	}
}

// Checker performs health checks on spawned processes
type Checker struct {
	config CheckConfig
	logger *logger.Logger
	client *http.Client
}

// NewChecker creates a new health checker
func NewChecker(cfg CheckConfig, log *logger.Logger) *Checker {
	if cfg.Interval == 0 {
		cfg.Interval = 1 * time.Second
	}
	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = 2 * time.Second
	}
	if cfg.SuccessThreshold == 0 {
		cfg.SuccessThreshold = 1
	}

	return &Checker{
		config: cfg,
		logger: log.WithComponent("health-checker"),
		client: &http.Client{
			Timeout: cfg.HTTPTimeout,
			// Don't follow redirects for health checks
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// WaitUntilReady waits until the process is ready or timeout occurs
// Returns error if the process doesn't become ready within the timeout
func (c *Checker) WaitUntilReady(ctx context.Context) error {
	c.logger.Info("starting health check",
		"url", c.config.URL,
		"timeout", c.config.Timeout,
		"interval", c.config.Interval)

	// Wait for initial delay if configured
	if c.config.InitialDelay > 0 {
		c.logger.Debug("waiting initial delay before first check",
			"delay", c.config.InitialDelay)
		select {
		case <-time.After(c.config.InitialDelay):
		case <-ctx.Done():
			return fmt.Errorf("context cancelled during initial delay: %w", ctx.Err())
		}
	}

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	ticker := time.NewTicker(c.config.Interval)
	defer ticker.Stop()

	attempt := 0
	consecutiveSuccesses := 0
	maxAttempts := int(c.config.Timeout / c.config.Interval)
	logEveryNAttempts := 15 // Log failed checks every ~15 seconds

	for {
		select {
		case <-timeoutCtx.Done():
			c.logger.Error("health check timeout",
				timeoutCtx.Err(),
				"attempts", attempt,
				"url", c.config.URL,
				"timeout", c.config.Timeout)
			return fmt.Errorf("health check timeout after %d attempts: %w",
				attempt, timeoutCtx.Err())

		case <-ticker.C:
			attempt++
			start := time.Now()

			err := c.check(timeoutCtx)
			latency := time.Since(start)

			if err == nil {
				consecutiveSuccesses++
				c.logger.HealthCheck(attempt, maxAttempts, c.config.URL, true, latency, nil)

				if consecutiveSuccesses >= c.config.SuccessThreshold {
					c.logger.Info("process is ready",
						"attempts", attempt,
						"url", c.config.URL,
						"total_time", time.Duration(attempt)*c.config.Interval)
					return nil
				}
			} else {
				consecutiveSuccesses = 0 // Reset on failure
				// Log at debug level every attempt, and at info level every N attempts
				c.logger.Debug("health check failed",
					"attempt", attempt,
					"max_attempts", maxAttempts,
					"url", c.config.URL,
					"latency", latency,
					"error", err)

				// Also log at info level every N attempts to reduce noise at info level
				if attempt%logEveryNAttempts == 0 || attempt == 1 {
					c.logger.HealthCheck(attempt, maxAttempts, c.config.URL, false, latency, err)
				}
			}
		}
	}
}

// check performs a single health check
func (c *Checker) check(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add user agent to identify health checks
	req.Header.Set("User-Agent", "jhub-app-proxy-health-check/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Consider any 2xx or 3xx status as healthy
	// Some apps might return 302 redirects on their health check endpoint
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return nil
	}

	return fmt.Errorf("unhealthy status code: %d", resp.StatusCode)
}

// CheckOnce performs a single health check (useful for testing)
func (c *Checker) CheckOnce(ctx context.Context) error {
	start := time.Now()
	err := c.check(ctx)
	latency := time.Since(start)

	c.logger.HealthCheck(1, 1, c.config.URL, err == nil, latency, err)
	return err
}
