// Package server provides HTTP server setup and lifecycle management
package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nebari-dev/jhub-app-proxy/pkg/api"
	"github.com/nebari-dev/jhub-app-proxy/pkg/auth"
	"github.com/nebari-dev/jhub-app-proxy/pkg/config"
	"github.com/nebari-dev/jhub-app-proxy/pkg/hub"
	"github.com/nebari-dev/jhub-app-proxy/pkg/interim"
	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
	"github.com/nebari-dev/jhub-app-proxy/pkg/process"
	"github.com/nebari-dev/jhub-app-proxy/pkg/proxy"
	"github.com/nebari-dev/jhub-app-proxy/pkg/router"
)

// Server represents the HTTP server and its components
type Server struct {
	httpServer     *http.Server
	manager        *process.ManagerWithLogs
	interimHandler *interim.Handler
	router         *router.Router
	logger         *logger.Logger
	config         *config.Config
	proxyPort      int
	subprocessPort int
	interimPath    string
}

// Config contains all dependencies needed to create a server
type Config struct {
	Manager        *process.ManagerWithLogs
	ProxyPort      int
	SubprocessPort int
	SubprocessURL  string
	AppConfig      *config.Config
	Logger         *logger.Logger
	Version        string
}

// New creates and configures the HTTP server with all handlers
func New(cfg Config) (*Server, error) {
	log := cfg.Logger

	// Get service prefix from environment
	servicePrefix := GetServicePrefix(log)
	interimBasePath := servicePrefix + interim.InterimPath
	appRootPath := servicePrefix + "/"

	// Setup HTTP handlers
	mux := http.NewServeMux()
	api.Version = cfg.Version

	// CRITICAL SECURITY: Determine if OAuth authentication is needed
	// Create a single shared OAuth middleware instance for both interim and proxy
	// This ensures state cookies are shared between redirectToLogin and handleCallback
	var sharedOAuthMW *auth.OAuthMiddleware
	needsOAuth := cfg.AppConfig.AuthType == "oauth" || cfg.AppConfig.InterimPageAuth

	if needsOAuth {
		var err error
		sharedOAuthMW, err = auth.NewOAuthMiddleware(log)
		if err != nil {
			return nil, fmt.Errorf("failed to create OAuth middleware: %w", err)
		}

		if cfg.AppConfig.AuthType == "oauth" {
			log.Info("OAuth authentication enabled for ALL routes (app + interim pages)")
		} else if cfg.AppConfig.InterimPageAuth {
			log.Info("OAuth authentication enabled for INTERIM PAGES ONLY (app is public)")
		}
	}

	// Determine if interim pages need authentication
	protectInterim := cfg.AppConfig.AuthType == "oauth" || cfg.AppConfig.InterimPageAuth

	// CRITICAL SECURITY: Register logs API handler with or without authentication
	logsHandler := api.NewLogsHandler(cfg.Manager, log)
	if protectInterim && sharedOAuthMW != nil {
		logsHandler.RegisterInterimRoutesWithAuth(mux, interimBasePath, sharedOAuthMW)
	} else {
		logsHandler.RegisterInterimRoutes(mux, interimBasePath)
		log.Warn("logs API NOT protected - sensitive logs exposed!", "path", interimBasePath+"/api/*")
	}

	// Create interim page handler
	interimHandler := interim.NewHandler(interim.Config{
		Manager:    cfg.Manager,
		Logger:     log,
		AppURLPath: appRootPath,
	})

	// CRITICAL SECURITY: Register OAuth callback handler first (must come before other routes)
	// The callback needs to be accessible from anywhere in the service prefix
	if sharedOAuthMW != nil {
		// Register OAuth callback at service prefix + /oauth_callback
		// This allows the OAuth flow to complete regardless of where it was initiated
		oauthCallbackPath := servicePrefix + "/oauth_callback"
		mux.HandleFunc(oauthCallbackPath, func(w http.ResponseWriter, r *http.Request) {
			// Use a minimal OAuth-wrapped handler that just handles the callback
			// After callback completes, it will redirect to the original URL
			sharedOAuthMW.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// This should never be reached - callback should redirect before getting here
				http.Redirect(w, r, servicePrefix+"/", http.StatusFound)
			})).ServeHTTP(w, r)
		})
		log.Info("OAuth callback registered", "path", oauthCallbackPath)
	}

	// CRITICAL SECURITY: Wrap interim handler with OAuth authentication if needed
	// Interim pages can expose sensitive subprocess logs!
	// Register both with and without trailing slash to handle all cases:
	// - /interim (exact path)
	// - /interim/ (prefix pattern for sub-paths like /interim/oauth_callback)
	if protectInterim && sharedOAuthMW != nil {
		wrappedHandler := sharedOAuthMW.Wrap(interimHandler)
		mux.Handle(interimBasePath, wrappedHandler)   // Exact path
		mux.Handle(interimBasePath+"/", wrappedHandler) // Prefix pattern
		log.Info("interim page protected with OAuth authentication", "path", interimBasePath)
	} else {
		mux.Handle(interimBasePath, interimHandler)   // Exact path
		mux.Handle(interimBasePath+"/", interimHandler) // Prefix pattern
		log.Warn("interim page NOT protected - sensitive logs exposed!", "path", interimBasePath)
	}

	// Create backend proxy handler
	proxyHandler, err := proxy.NewHandler(
		cfg.Manager,
		cfg.SubprocessURL,
		cfg.AppConfig.AuthType,
		cfg.AppConfig.Progressive,
		servicePrefix,
		cfg.AppConfig.StripPrefix,
		log,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy handler: %w", err)
	}

	// Create main router
	mainRouter := router.New(router.Config{
		Logger:          log,
		Mux:             mux,
		InterimHandler:  interimHandler,
		ProxyHandler:    proxyHandler,
		Manager:         cfg.Manager,
		ServicePrefix:   servicePrefix,
		InterimBasePath: interimBasePath,
		AppRootPath:     appRootPath,
		SubprocessURL:   cfg.SubprocessURL,
	})

	// Create HTTP server
	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.ProxyPort),
		Handler: mainRouter,
	}

	return &Server{
		httpServer:     httpServer,
		manager:        cfg.Manager,
		interimHandler: interimHandler,
		router:         mainRouter,
		logger:         log,
		config:         cfg.AppConfig,
		proxyPort:      cfg.ProxyPort,
		subprocessPort: cfg.SubprocessPort,
		interimPath:    interimBasePath,
	}, nil
}

// Start starts the HTTP server in a goroutine
func (s *Server) Start() {
	go func() {
		s.logger.Info("starting proxy server", "port", s.proxyPort)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("proxy server failed", err)
		}
	}()

	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", s.proxyPort)
	s.logger.Info("proxy server ready",
		"proxy_url", proxyURL,
		"logs_api", fmt.Sprintf("%s/api/logs", proxyURL),
		"internal_port", s.subprocessPort)
}

// StartSubprocess starts the managed subprocess
func (s *Server) StartSubprocess(ctx context.Context, cmd []string) {
	s.logger.Info("starting subprocess", "command", cmd)

	if err := s.manager.Start(ctx); err != nil {
		s.logger.Error("failed to start subprocess", err)
		s.manager.AddErrorLog(fmt.Sprintf("ERROR: Failed to start process: %s", err.Error()))
		s.manager.AddErrorLog(fmt.Sprintf("Command: %v", cmd))
		return
	}

	s.logger.Info("subprocess started successfully",
		"pid", s.manager.GetPID(),
		"internal_port", s.subprocessPort)

	appURL := fmt.Sprintf("http://127.0.0.1:%d", s.proxyPort)
	s.logger.Info("application ready",
		"app_url", appURL,
		"interim_page", fmt.Sprintf("%s%s", appURL, s.interimPath),
		"pid", s.manager.GetPID())

	s.interimHandler.MarkAppDeployed()

	if s.config.AuthType == "oauth" {
		if err := startActivityReporter(ctx, s.config, s.logger); err != nil {
			s.logger.Warn("failed to start activity reporter (continuing anyway)", "error", err)
		}
	}
}

// Shutdown performs graceful shutdown of the server and subprocess
func (s *Server) Shutdown() {
	s.logger.ShutdownBanner("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if s.manager.IsRunning() {
		s.logger.Info("stopping subprocess")
		if err := s.manager.Stop(); err != nil {
			s.logger.Error("failed to stop subprocess", err)
		}
	}

	s.logger.Info("stopping proxy server")
	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		s.logger.Error("proxy server shutdown error", err)
	}

	s.logger.Info("shutdown complete")
}

// SetupSignalHandling configures signal handlers for graceful shutdown
func SetupSignalHandling(ctx context.Context, cancel context.CancelFunc, log *logger.Logger) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Info("received signal, initiating graceful shutdown (press Ctrl+C again to force quit)", "signal", sig)
		cancel()

		sig = <-sigChan
		log.Warn("received second signal, forcing immediate exit", "signal", sig)
		os.Exit(1)
	}()
}

// GetServicePrefix retrieves and processes the JupyterHub service prefix from environment
func GetServicePrefix(log *logger.Logger) string {
	servicePrefix := os.Getenv("JUPYTERHUB_SERVICE_PREFIX")
	if servicePrefix != "" {
		servicePrefix = strings.TrimSuffix(servicePrefix, "/")
		log.Info("using JupyterHub service prefix", "prefix", servicePrefix)
	}
	return servicePrefix
}

// startActivityReporter starts the JupyterHub activity reporter
func startActivityReporter(ctx context.Context, cfg *config.Config, log *logger.Logger) error {
	hubClient, err := hub.NewClientFromEnv(log)
	if err != nil {
		return fmt.Errorf("failed to create hub client: %w", err)
	}

	if err := hubClient.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping hub: %w", err)
	}

	interval := 5 * time.Minute
	_ = hubClient.StartActivityReporter(ctx, interval, cfg.ForceAlive)

	log.Info("activity reporter started",
		"interval", interval,
		"force_alive", cfg.ForceAlive)

	return nil
}
