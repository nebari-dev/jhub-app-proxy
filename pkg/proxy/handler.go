// Package proxy provides intelligent HTTP proxying with startup log viewing
package proxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
	"github.com/nebari-dev/jhub-app-proxy/pkg/process"
	"github.com/nebari-dev/jhub-app-proxy/pkg/ui"
)

// Handler is an intelligent proxy that shows logs until the app is ready
type Handler struct {
	manager       *process.ManagerWithLogs
	upstreamURL   string
	reverseProxy  *httputil.ReverseProxy
	logger        *logger.Logger
	logsHandler   http.Handler
}

// NewHandler creates a new proxy handler
func NewHandler(manager *process.ManagerWithLogs, upstreamURL string, logsHandler http.Handler, log *logger.Logger) *Handler {
	target, _ := url.Parse(upstreamURL)

	return &Handler{
		manager:      manager,
		upstreamURL:  upstreamURL,
		reverseProxy: httputil.NewSingleHostReverseProxy(target),
		logger:       log,
		logsHandler:  logsHandler,
	}
}

// ServeHTTP implements http.Handler
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Always serve API endpoints
	if strings.HasPrefix(r.URL.Path, "/api/") {
		h.logsHandler.ServeHTTP(w, r)
		return
	}

	// If app is not running yet, show log viewer
	if !h.manager.IsRunning() {
		h.serveLogViewer(w, r)
		return
	}

	// App is running, proxy to it
	h.reverseProxy.ServeHTTP(w, r)
}

// serveLogViewer serves the log viewer HTML with 200 status
// Always returns 200 so JupyterHub redirects users immediately to see the log viewer
func (h *Handler) serveLogViewer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK) // 200 - always ready (shows logs while app starts)
	fmt.Fprint(w, ui.LogsHTML)
}
