// Package proxy provides intelligent HTTP proxying with startup log viewing
package proxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/nebari-dev/jhub-app-proxy/pkg/auth"
	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
	"github.com/nebari-dev/jhub-app-proxy/pkg/process"
	"github.com/nebari-dev/jhub-app-proxy/pkg/ui"
)

// Handler is an intelligent proxy that shows logs until the app is ready
type Handler struct {
	manager      *process.ManagerWithLogs
	upstreamURL  string
	reverseProxy *httputil.ReverseProxy
	logger       *logger.Logger
	logsHandler  http.Handler
	authType     string
	oauthMW      *auth.OAuthMiddleware
	progressive  bool
}

// NewHandler creates a new proxy handler
func NewHandler(manager *process.ManagerWithLogs, upstreamURL string, logsHandler http.Handler, authType string, progressive bool, log *logger.Logger) (*Handler, error) {
	target, _ := url.Parse(upstreamURL)

	var oauthMW *auth.OAuthMiddleware
	if authType == "oauth" {
		var err error
		oauthMW, err = auth.NewOAuthMiddleware(log)
		if err != nil {
			return nil, fmt.Errorf("failed to create OAuth middleware: %w", err)
		}
	}

	h := &Handler{
		manager:     manager,
		upstreamURL: upstreamURL,
		logger:      log,
		logsHandler: logsHandler,
		authType:    authType,
		oauthMW:     oauthMW,
		progressive: progressive,
	}

	// Configure reverse proxy
	if progressive {
		// For progressive mode, use custom transport with flushing
		h.reverseProxy = httputil.NewSingleHostReverseProxy(target)
		h.reverseProxy.FlushInterval = -1 // Flush immediately on each write
	} else {
		h.reverseProxy = httputil.NewSingleHostReverseProxy(target)
	}

	return h, nil
}

// ServeHTTP implements http.Handler
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handler := http.HandlerFunc(h.serve)

	// Wrap with OAuth if enabled
	if h.oauthMW != nil {
		h.oauthMW.Wrap(handler).ServeHTTP(w, r)
	} else {
		handler.ServeHTTP(w, r)
	}
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	// Serve API endpoints
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
