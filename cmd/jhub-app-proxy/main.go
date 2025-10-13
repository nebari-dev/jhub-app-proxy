// JHub Apps Spawner - A modern replacement for jhsingle-native-proxy
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/nebari-dev/jhub-app-proxy/pkg/command"
	"github.com/nebari-dev/jhub-app-proxy/pkg/config"
	"github.com/nebari-dev/jhub-app-proxy/pkg/git"
	"github.com/nebari-dev/jhub-app-proxy/pkg/health"
	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
	"github.com/nebari-dev/jhub-app-proxy/pkg/port"
	"github.com/nebari-dev/jhub-app-proxy/pkg/process"
	"github.com/nebari-dev/jhub-app-proxy/pkg/server"
	"github.com/spf13/cobra"
)

var (
	// Version information (set during build)
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	rootCmd, cfg, err := config.NewFromFlags(Version, BuildTime)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create config: %v\n", err)
		os.Exit(1)
	}

	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		cfg.Command = args
		return run(cfg)
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cfg *config.Config) error {
	// Normalize port configuration
	cfg.NormalizePort()

	// Initialize logger
	logCfg := logger.Config{
		Level:      logger.Level(cfg.LogLevel),
		Format:     logger.Format(cfg.LogFormat),
		ShowCaller: cfg.ShowCaller,
		TimeFormat: "2006-01-02 15:04:05.000",
	}
	log := logger.New(logCfg)

	// Log port configuration
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

	// Setup context and signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server.SetupSignalHandling(ctx, cancel, log)

	// Handle git repository cloning if specified
	if cfg.Repo != "" {
		if err := handleGitClone(cfg, log); err != nil {
			return fmt.Errorf("git clone failed: %w", err)
		}
	}

	// Build command with conda activation if needed
	cmdBuilder := command.NewBuilder(log)
	cmd, err := cmdBuilder.Build(cfg.Command, cfg.CondaEnv)
	if err != nil {
		return fmt.Errorf("failed to build command: %w", err)
	}

	// Allocate ports
	proxyPort := cfg.Port
	log.Info("proxy will listen on port", "port", proxyPort)

	subprocessPort, err := port.Allocate(cfg.DestPort)
	if err != nil {
		return fmt.Errorf("failed to allocate subprocess port: %w", err)
	}
	log.Info("allocated internal port for subprocess", "port", subprocessPort)

	// Substitute port placeholders
	cmd = command.SubstitutePort(cmd, subprocessPort)

	// Create health checker
	upstreamURL := fmt.Sprintf("http://127.0.0.1:%d%s", subprocessPort, cfg.ReadyCheckPath)
	healthCfg := health.DefaultCheckConfig(upstreamURL)
	healthCfg.Timeout = time.Duration(cfg.ReadyTimeout) * time.Second
	healthChecker := health.NewChecker(healthCfg, log)

	// Create process manager with log capture
	mgr, err := process.NewManagerWithLogs(
		process.Config{
			Command: cmd,
			Env:     command.BuildEnv(),
			WorkDir: cfg.WorkDir,
			ReadyCheck: func(ctx context.Context) error {
				return healthChecker.WaitUntilReady(ctx)
			},
		},
		process.LogCaptureConfig{
			Enabled:    true,
			BufferSize: cfg.LogBufferSize,
		},
		log,
	)
	if err != nil {
		return fmt.Errorf("failed to create process manager: %w", err)
	}

	// Create and start HTTP server
	subprocessURL := fmt.Sprintf("http://127.0.0.1:%d", subprocessPort)
	srv, err := server.New(server.Config{
		Manager:        mgr,
		ProxyPort:      proxyPort,
		SubprocessPort: subprocessPort,
		SubprocessURL:  subprocessURL,
		AppConfig:      cfg,
		Logger:         log,
		Version:        Version,
	})
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	srv.Start()
	defer srv.Shutdown()

	// Start subprocess
	go srv.StartSubprocess(ctx, cmd)

	// Wait for shutdown
	<-ctx.Done()
	return nil
}

func handleGitClone(cfg *config.Config, log *logger.Logger) error {
	gitMgr := git.NewManager(log)

	if !gitMgr.IsGitInstalled() {
		return fmt.Errorf("git is not installed")
	}

	cloneCfg := git.CloneConfig{
		RepoURL:  cfg.Repo,
		Branch:   cfg.RepoBranch,
		DestPath: cfg.RepoFolder,
		Depth:    1,
	}

	return gitMgr.Clone(cloneCfg)
}
