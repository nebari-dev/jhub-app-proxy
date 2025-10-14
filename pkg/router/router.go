// Package router provides intelligent HTTP request routing
package router

import (
	"net/http"
	"strings"

	"github.com/nebari-dev/jhub-app-proxy/pkg/interim"
	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
	"github.com/nebari-dev/jhub-app-proxy/pkg/process"
	"github.com/nebari-dev/jhub-app-proxy/pkg/proxy"
)

// Router handles intelligent routing between interim page, logs API, and backend application
type Router struct {
	log               *logger.Logger
	mux               *http.ServeMux
	interimHandler    *interim.Handler
	proxyHandler      *proxy.Handler
	mgr               *process.ManagerWithLogs
	servicePrefix     string
	interimBasePath   string
	appRootPath       string
	subprocessURL     string
	oauthCallbackPath string // Empty if OAuth disabled for jhub-app-proxy
}

// Config contains configuration for the router
type Config struct {
	Logger            *logger.Logger
	Mux               *http.ServeMux
	InterimHandler    *interim.Handler
	ProxyHandler      *proxy.Handler
	Manager           *process.ManagerWithLogs
	ServicePrefix     string
	InterimBasePath   string
	AppRootPath       string
	SubprocessURL     string
	OAuthCallbackPath string // Empty if OAuth disabled for jhub-app-proxy
}

// New creates a new router with the given configuration
func New(cfg Config) *Router {
	return &Router{
		log:               cfg.Logger,
		mux:               cfg.Mux,
		interimHandler:    cfg.InterimHandler,
		proxyHandler:      cfg.ProxyHandler,
		mgr:               cfg.Manager,
		servicePrefix:     cfg.ServicePrefix,
		interimBasePath:   cfg.InterimBasePath,
		appRootPath:       cfg.AppRootPath,
		subprocessURL:     cfg.SubprocessURL,
		oauthCallbackPath: cfg.OAuthCallbackPath,
	}
}

// ServeHTTP implements http.Handler with intelligent routing logic
func (rtr *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	rtr.log.Info("incoming request",
		"method", r.Method,
		"path", path,
		"remote_addr", r.RemoteAddr)

	// Route 0: OAuth callback for jhub-app-proxy (only when OAuth is enabled)
	// CRITICAL: Only intercept if OAuth is enabled AND app is not running
	// This ensures the callback is always routed to the backend app when it's running, its
	// specially useful when the backend app also uses OAuth (e.g., JupyterLab), and we don't
	// want to interfere with its OAuth flow.
	// When app is running, proxy the callback to the backend app (e.g., JupyterLab)
	if rtr.oauthCallbackPath != "" && path == rtr.oauthCallbackPath {
		if !rtr.mgr.IsRunning() {
			rtr.log.Info("routing OAuth callback to jhub-app-proxy (app not running)",
				"path", path)
			rtr.mux.ServeHTTP(w, r)
			return
		}
		rtr.log.Info("proxying OAuth callback to backend app (app running)",
			"path", path)
		// Fall through to proxy
	}

	// Route 1: Interim page and its API (during startup + grace period)
	if strings.HasPrefix(path, rtr.interimBasePath) {
		rtr.handleInterimRoute(w, r, path)
		return
	}

	// Route 2: Application routes
	if !rtr.validateServicePrefix(w, r, path) {
		return
	}

	// Route to interim page or proxy based on app state
	if !rtr.mgr.IsRunning() {
		rtr.handleAppStarting(w, r, path)
		return
	}

	rtr.handleAppRunning(w, r, path)
}

// handleInterimRoute routes requests to the interim infrastructure or redirects if grace period expired
func (rtr *Router) handleInterimRoute(w http.ResponseWriter, r *http.Request, path string) {
	if rtr.interimHandler.ShouldServeLogsAPI() {
		rtr.log.Info("routing to interim infrastructure",
			"path", path,
			"reason", "app not running or in grace period")
		rtr.mux.ServeHTTP(w, r)
		return
	}

	// Grace period expired - redirect to app
	rtr.log.Info("redirecting from interim to app",
		"from", path,
		"to", rtr.appRootPath,
		"reason", "grace period expired")
	http.Redirect(w, r, rtr.appRootPath, http.StatusTemporaryRedirect)
}

// validateServicePrefix checks if the request path matches the service prefix (if configured)
// Returns false if the path is invalid and response has been written
func (rtr *Router) validateServicePrefix(w http.ResponseWriter, r *http.Request, path string) bool {
	if rtr.servicePrefix != "" && !strings.HasPrefix(path, rtr.servicePrefix+"/") {
		rtr.log.Info("path does not match service prefix",
			"path", path,
			"expected_prefix", rtr.servicePrefix,
			"response", "404")
		http.NotFound(w, r)
		return false
	}
	return true
}

// handleAppStarting serves the interim page when the app is not yet running
func (rtr *Router) handleAppStarting(w http.ResponseWriter, r *http.Request, path string) {
	rtr.log.Info("serving interim page (app not running)",
		"path", path,
		"app_status", "not_running")
	rtr.interimHandler.ServeHTTP(w, r)
}

// handleAppRunning proxies the request to the backend application
func (rtr *Router) handleAppRunning(w http.ResponseWriter, r *http.Request, path string) {
	rtr.log.Info("proxying to backend",
		"path", path,
		"backend_url", rtr.subprocessURL,
		"app_status", "running")
	rtr.proxyHandler.ServeHTTP(w, r)
}
