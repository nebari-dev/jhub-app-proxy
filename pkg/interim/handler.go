// Package interim provides the interim log viewer page functionality.
//
// The interim page is shown at /_temp/jhub-app-proxy while the backend
// application is starting up. It displays real-time logs and automatically
// redirects to the application once it's ready.
//
// Design principles:
// - Clean separation from application URLs
// - No path conflicts with backend apps
// - Grace period for final log fetching after app starts
package interim

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
	"github.com/nebari-dev/jhub-app-proxy/pkg/process"
	"github.com/nebari-dev/jhub-app-proxy/pkg/ui"
)

const (
	// InterimPath is the base path for the interim log viewer
	InterimPath = "/_temp/jhub-app-proxy"

	// GracePeriod is how long the interim page remains accessible after app deployment
	// This allows the interim page to fetch final logs before redirecting
	GracePeriod = 10 * time.Second
)

// Handler manages the interim log viewer page
type Handler struct {
	manager *process.ManagerWithLogs
	logger  *logger.Logger

	// Deployment tracking for grace period
	mu              sync.RWMutex
	deploymentTime  time.Time
	appURLPath      string // The path to redirect to after app is ready (e.g., "/" or "/user/admin/app/")
}

// Config contains configuration for the interim handler
type Config struct {
	Manager    *process.ManagerWithLogs
	Logger     *logger.Logger
	AppURLPath string // Path to redirect to (e.g., "/" or "/user/admin/app/")
}

// NewHandler creates a new interim page handler
func NewHandler(cfg Config) *Handler {
	return &Handler{
		manager:    cfg.Manager,
		logger:     cfg.Logger.WithComponent("interim-handler"),
		appURLPath: cfg.AppURLPath,
	}
}

// ServeHTTP serves the interim log viewer HTML page
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check if we're in grace period (app deployed but page still accessible)
	if h.isInGracePeriod() {
		h.logger.Info("serving interim page in grace period")
	} else if h.manager.IsRunning() {
		// App is running and grace period expired - redirect to app
		h.logger.Info("app running and grace period expired, redirecting to app",
			"redirect_to", h.appURLPath)
		http.Redirect(w, r, h.appURLPath, http.StatusTemporaryRedirect)
		return
	}

	// Serve the interim log viewer page with injected redirect URL
	h.logger.Info("serving interim page",
		"request_path", r.URL.Path,
		"app_url", h.appURLPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusOK)

	// Inject the app URL into the HTML via a meta tag that JavaScript can read
	html := strings.Replace(ui.LogsHTML, "<title>",
		fmt.Sprintf("<meta name=\"app-redirect-url\" content=\"%s\">\n    <title>", h.appURLPath), 1)
	fmt.Fprint(w, html)
}

// MarkAppDeployed marks the timestamp when the app became ready
// This starts the grace period timer
func (h *Handler) MarkAppDeployed() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.deploymentTime.IsZero() {
		h.deploymentTime = time.Now()
		h.logger.Info("app deployed, starting grace period",
			"grace_period", GracePeriod,
			"expires_at", h.deploymentTime.Add(GracePeriod))
	}
}

// IsInGracePeriod returns true if we're within the grace period after deployment
func (h *Handler) isInGracePeriod() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.deploymentTime.IsZero() {
		return false
	}

	elapsed := time.Since(h.deploymentTime)
	return elapsed < GracePeriod
}

// ShouldServeLogsAPI returns true if the logs API should still be accessible
// This is true when either:
// 1. App is not running yet, OR
// 2. App is running but we're in grace period (for final log fetching)
func (h *Handler) ShouldServeLogsAPI() bool {
	if !h.manager.IsRunning() {
		return true
	}
	return h.isInGracePeriod()
}
