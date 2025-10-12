// JHub Apps Spawner - A modern replacement for jhsingle-native-proxy
package main

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
	"github.com/nebari-dev/jhub-app-proxy/pkg/conda"
	"github.com/nebari-dev/jhub-app-proxy/pkg/git"
	"github.com/nebari-dev/jhub-app-proxy/pkg/health"
	"github.com/nebari-dev/jhub-app-proxy/pkg/hub"
	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
	"github.com/nebari-dev/jhub-app-proxy/pkg/port"
	"github.com/nebari-dev/jhub-app-proxy/pkg/process"
	"github.com/nebari-dev/jhub-app-proxy/pkg/proxy"
	"github.com/spf13/cobra"
)

var (
	// Version information (set during build)
	Version   = "dev"
	BuildTime = "unknown"
)

// Config holds application configuration
type Config struct {
	// Authentication
	AuthType string // "oauth", "none"

	// Process
	Command    []string
	DestPort   int
	CondaEnv   string
	WorkDir    string
	ForceAlive bool

	// Git
	Repo       string
	RepoFolder string
	RepoBranch string

	// Health Check
	ReadyCheckPath string
	ReadyTimeout   int // seconds

	// Logging
	LogLevel      string
	LogFormat     string
	LogBufferSize int
	ShowCaller    bool

	// Server
	Port       int // Port for proxy server (what JupyterHub expects)
	ListenPort int // Deprecated: use Port instead

	// Voila-specific
	Progressive bool
}

func main() {
	cfg := &Config{}

	rootCmd := &cobra.Command{
		Use:     "jhub-app-proxy [flags] -- command [args...]",
		Short:   "Process spawner with OAuth2 authentication for JupyterHub apps",
		Version: fmt.Sprintf("%s (built %s)", Version, BuildTime),
		Long: `Spawns and manages web application processes with OAuth2 authentication,
health monitoring, log capture, and JupyterHub integration.

Framework-agnostic - works with any web application (Streamlit, Voila, Panel, etc).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// If no command provided, show help
			if len(args) == 0 {
				return cmd.Help()
			}
			// Remaining args are the command to run
			cfg.Command = args
			return run(cfg)
		},
	}

	// Core flags
	rootCmd.Flags().StringVar(&cfg.AuthType, "authtype", "oauth",
		"Authentication type (oauth, none)")
	rootCmd.Flags().IntVar(&cfg.Port, "port", 0,
		"Port for proxy server to listen on (what JupyterHub expects)")
	rootCmd.Flags().IntVar(&cfg.ListenPort, "listen-port", 0,
		"Deprecated: use --port instead")
	rootCmd.Flags().IntVar(&cfg.DestPort, "destport", 0,
		"Internal subprocess port (0 = random)")

	// Process management flags
	rootCmd.Flags().StringVar(&cfg.CondaEnv, "conda-env", "",
		"Conda environment to activate")
	rootCmd.Flags().StringVar(&cfg.WorkDir, "workdir", "",
		"Working directory for the process")
	rootCmd.Flags().BoolVar(&cfg.ForceAlive, "force-alive", true,
		"Force keep-alive (prevent idle culling)")

	// Legacy compatibility flag that sets force-alive to false
	rootCmd.Flags().BoolVar(&cfg.ForceAlive, "no-force-alive", false,
		"Disable force keep-alive (report only real activity)")

	// Git repository flags
	rootCmd.Flags().StringVar(&cfg.Repo, "repo", "",
		"Git repository URL to clone")
	rootCmd.Flags().StringVar(&cfg.RepoFolder, "repofolder", "",
		"Destination folder for git clone")
	rootCmd.Flags().StringVar(&cfg.RepoBranch, "repobranch", "main",
		"Git branch to checkout")

	// Health check flags
	rootCmd.Flags().StringVar(&cfg.ReadyCheckPath, "ready-check-path", "/",
		"Health check path (e.g., /, /health, /voila/static/)")
	rootCmd.Flags().IntVar(&cfg.ReadyTimeout, "ready-timeout", 300,
		"Health check timeout in seconds")

	// Logging flags
	rootCmd.Flags().StringVar(&cfg.LogLevel, "log-level", "info",
		"Log level (debug, info, warn, error)")
	rootCmd.Flags().StringVar(&cfg.LogFormat, "log-format", "json",
		"Log format (json, pretty)")
	rootCmd.Flags().IntVar(&cfg.LogBufferSize, "log-buffer-size", 1000,
		"Number of subprocess log lines to keep in memory")
	rootCmd.Flags().BoolVar(&cfg.ShowCaller, "log-caller", false,
		"Show file:line in logs")

	// Optional flags
	rootCmd.Flags().BoolVar(&cfg.Progressive, "progressive", false,
		"Enable progressive response streaming (for Voila)")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cfg *Config) error {
	// Handle backward compatibility: --listen-port → --port
	if cfg.Port == 0 && cfg.ListenPort != 0 {
		cfg.Port = cfg.ListenPort
	}
	// Try to get port from environment variable (JupyterHub sets this)
	if cfg.Port == 0 {
		if envPort := os.Getenv("JHUB_APPS_SPAWNER_PORT"); envPort != "" {
			if port, err := fmt.Sscanf(envPort, "%d", &cfg.Port); err == nil && port == 1 {
				// Port successfully parsed from environment - will log after logger is initialized
			}
		}
	}
	// Default port if still not set
	if cfg.Port == 0 {
		cfg.Port = 8888
	}

	// Initialize logger
	logCfg := logger.Config{
		Level:      logger.Level(cfg.LogLevel),
		Format:     logger.Format(cfg.LogFormat),
		ShowCaller: cfg.ShowCaller,
		TimeFormat: "2006-01-02 15:04:05.000", // JupyterHub-style format with milliseconds
	}
	log := logger.New(logCfg)

	// Log if port was loaded from environment
	if envPort := os.Getenv("JHUB_APPS_SPAWNER_PORT"); envPort != "" {
		log.Info("JHUB_APPS_SPAWNER_PORT environment variable", "value", envPort, "parsed_port", cfg.Port)
	} else {
		log.Info("JHUB_APPS_SPAWNER_PORT not set, using default or flag value", "port", cfg.Port)
	}

	// Print startup banner
	log.StartupBanner(Version, map[string]interface{}{
		"auth_type":        cfg.AuthType,
		"port":             cfg.Port,
		"dest_port":        cfg.DestPort,
		"conda_env":        cfg.CondaEnv,
		"log_level":        cfg.LogLevel,
		"log_format":       cfg.LogFormat,
		"log_buffer_size":  cfg.LogBufferSize,
		"ready_check_path": cfg.ReadyCheckPath,
		"progressive":      cfg.Progressive,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Cancel context on signal (allows interrupting startup)
	// Second signal forces immediate exit
	go func() {
		sig := <-sigChan
		log.Info("received signal, initiating graceful shutdown (press Ctrl+C again to force quit)", "signal", sig)
		cancel()

		// Wait for second signal to force exit
		sig = <-sigChan
		log.Warn("received second signal, forcing immediate exit", "signal", sig)
		os.Exit(1)
	}()

	// Handle git repository cloning if specified
	if cfg.Repo != "" {
		if err := handleGitClone(cfg, log); err != nil {
			return fmt.Errorf("git clone failed: %w", err)
		}
	}

	// Build the command with conda activation if needed
	command, err := buildCommand(cfg, log)
	if err != nil {
		return fmt.Errorf("failed to build command: %w", err)
	}

	// Use the specified port for proxy (what JupyterHub expects)
	proxyPort := cfg.Port
	log.Info("proxy will listen on port", "port", proxyPort)

	// Allocate internal port for subprocess
	subprocessPort, err := port.Allocate(cfg.DestPort)
	if err != nil {
		return fmt.Errorf("failed to allocate subprocess port: %w", err)
	}
	log.Info("allocated internal port for subprocess", "port", subprocessPort)

	// Substitute {port} placeholder in command with subprocess port
	command = substitutePort(command, subprocessPort)

	// Build upstream URL for health check (against subprocess)
	upstreamURL := fmt.Sprintf("http://127.0.0.1:%d%s", subprocessPort, cfg.ReadyCheckPath)

	// Create health checker
	healthCfg := health.DefaultCheckConfig(upstreamURL)
	healthCfg.Timeout = time.Duration(cfg.ReadyTimeout) * time.Second
	healthChecker := health.NewChecker(healthCfg, log)

	// Create process manager with log capture
	processCfg := process.Config{
		Command:  command,
		Env:      buildEnv(cfg, subprocessPort),
		WorkDir:  cfg.WorkDir,
		ReadyCheck: func(ctx context.Context) error {
			return healthChecker.WaitUntilReady(ctx)
		},
	}

	logCaptureCfg := process.LogCaptureConfig{
		Enabled:    true,
		BufferSize: cfg.LogBufferSize,
	}

	mgr, err := process.NewManagerWithLogs(processCfg, logCaptureCfg, log)
	if err != nil {
		return fmt.Errorf("failed to create process manager: %w", err)
	}

	// Setup log API endpoints
	mux := http.NewServeMux()
	api.Version = Version // Set version for API responses
	logsHandler := api.NewLogsHandler(mgr, log)
	logsHandler.RegisterRoutes(mux)

	// Create intelligent proxy that shows logs until app is ready
	// Proxy forwards to subprocess on internal port
	subprocessURL := fmt.Sprintf("http://127.0.0.1:%d", subprocessPort)
	proxyHandler, err := proxy.NewHandler(mgr, subprocessURL, mux, cfg.AuthType, cfg.Progressive, log)
	if err != nil {
		return fmt.Errorf("failed to create proxy handler: %w", err)
	}

	// Handle JupyterHub service prefix (e.g., /user/admin/servername)
	// Pass through the full path to the backend application unchanged.
	// The backend app is responsible for handling the service prefix.
	var handler http.Handler = proxyHandler
	if servicePrefix := os.Getenv("JUPYTERHUB_SERVICE_PREFIX"); servicePrefix != "" {
		// Strip trailing slash for consistent behavior
		servicePrefix = strings.TrimSuffix(servicePrefix, "/")
		log.Info("using JupyterHub service prefix", "prefix", servicePrefix)
		// Note: We do NOT strip the prefix - apps like JupyterLab handle it themselves
	}

	// Start proxy server on the port JupyterHub expects
	apiServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", proxyPort),
		Handler: handler,
	}

	go func() {
		log.Info("starting proxy server", "port", proxyPort)
		if err := apiServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("proxy server failed", err)
		}
	}()

	// Ensure cleanup on exit
	defer func() {
		log.ShutdownBanner("shutting down")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		// Stop subprocess first if running
		if mgr.IsRunning() {
			log.Info("stopping subprocess")
			if err := mgr.Stop(); err != nil {
				log.Error("failed to stop subprocess", err)
			}
		}

		// Stop proxy server
		log.Info("stopping proxy server")
		if err := apiServer.Shutdown(shutdownCtx); err != nil {
			log.Error("proxy server shutdown error", err)
		}

		log.Info("shutdown complete")
	}()

	// Start the subprocess in background (don't exit on failure - show logs instead)
	log.Info("starting subprocess", "command", command)
	go func() {
		if err := mgr.Start(ctx); err != nil {
			log.Error("failed to start subprocess", err)
			// Add error to log buffer so users can see it
			mgr.AddErrorLog(fmt.Sprintf("ERROR: Failed to start process: %s", err.Error()))
			mgr.AddErrorLog(fmt.Sprintf("Command: %v", command))
			// Don't exit - keep proxy server running to show logs
		} else {
			log.Info("subprocess started successfully",
				"pid", mgr.GetPID(),
				"internal_port", subprocessPort)

			// Log application URLs with separators
			appURL := fmt.Sprintf("http://127.0.0.1:%d", proxyPort)
			log.Info("==================================================")
			log.Info("application ready",
				"app_url", appURL,
				"logs_api", fmt.Sprintf("%s/api/logs", appURL),
				"pid", mgr.GetPID())
			log.Info("==================================================")

			// Start JupyterHub activity reporter if in oauth mode
			if cfg.AuthType == "oauth" {
				if err := startActivityReporter(ctx, cfg, log); err != nil {
					log.Warn("failed to start activity reporter (continuing anyway)", "error", err)
				}
			}
		}
	}()

	// Log proxy server startup with separators
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", proxyPort)
	log.Info("==================================================")
	log.Info("proxy server ready",
		"proxy_url", proxyURL,
		"logs_api", fmt.Sprintf("%s/api/logs", proxyURL),
		"internal_port", subprocessPort)
	log.Info("==================================================")

	// Wait for shutdown signal (context will be cancelled by signal handler)
	<-ctx.Done()
	return nil
}

func handleGitClone(cfg *Config, log *logger.Logger) error {
	gitMgr := git.NewManager(log)

	if !gitMgr.IsGitInstalled() {
		return fmt.Errorf("git is not installed")
	}

	cloneCfg := git.CloneConfig{
		RepoURL:  cfg.Repo,
		Branch:   cfg.RepoBranch,
		DestPath: cfg.RepoFolder,
		Depth:    1, // Shallow clone for faster startup
	}

	return gitMgr.Clone(cloneCfg)
}

func buildCommand(cfg *Config, log *logger.Logger) ([]string, error) {
	command := cfg.Command
	if len(command) == 0 {
		return nil, fmt.Errorf("no command specified")
	}

	// Apply conda activation if specified
	if cfg.CondaEnv != "" {
		condaMgr := conda.NewManager(log)
		var err error
		command, err = condaMgr.BuildActivationCommand(cfg.CondaEnv, command)
		if err != nil {
			return nil, fmt.Errorf("failed to build conda activation: %w", err)
		}
	}

	return command, nil
}

func buildEnv(cfg *Config, allocatedPort int) map[string]string {
	env := make(map[string]string)

	// Pass through JupyterHub environment variables
	jupyterHubEnvVars := []string{
		"JUPYTERHUB_API_TOKEN",
		"JUPYTERHUB_API_URL",
		"JUPYTERHUB_BASE_URL",
		"JUPYTERHUB_USER",
		"JUPYTERHUB_SERVER_NAME",
		"JUPYTERHUB_SERVICE_PREFIX",
		"JUPYTERHUB_GROUP",
	}

	for _, key := range jupyterHubEnvVars {
		if val := os.Getenv(key); val != "" {
			env[key] = val
		}
	}

	return env
}

// substitutePort replaces jhsingle-native-proxy style placeholders in command arguments
// Handles: {port} → actual port, {-} → -, {--} → --, and strips surrounding quotes
func substitutePort(command []string, allocatedPort int) []string {
	result := make([]string, len(command))
	portStr := fmt.Sprintf("%d", allocatedPort)

	for i, arg := range command {
		processed := arg

		// Replace port placeholder
		processed = strings.ReplaceAll(processed, "{port}", portStr)

		// Replace dash placeholders (jhsingle-native-proxy compatibility)
		processed = strings.ReplaceAll(processed, "{-}", "-")
		processed = strings.ReplaceAll(processed, "{--}", "--")

		// Strip surrounding single quotes if present
		if len(processed) >= 2 && processed[0] == '\'' && processed[len(processed)-1] == '\'' {
			processed = processed[1 : len(processed)-1]
		}

		// Strip surrounding double quotes if present
		if len(processed) >= 2 && processed[0] == '"' && processed[len(processed)-1] == '"' {
			processed = processed[1 : len(processed)-1]
		}

		result[i] = processed
	}

	return result
}

func startActivityReporter(ctx context.Context, cfg *Config, log *logger.Logger) error {
	hubClient, err := hub.NewClientFromEnv(log)
	if err != nil {
		return fmt.Errorf("failed to create hub client: %w", err)
	}

	// Test hub connection
	if err := hubClient.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping hub: %w", err)
	}

	// Start activity reporter
	interval := 5 * time.Minute
	_ = hubClient.StartActivityReporter(ctx, interval, cfg.ForceAlive)

	log.Info("activity reporter started",
		"interval", interval,
		"force_alive", cfg.ForceAlive)

	return nil
}
