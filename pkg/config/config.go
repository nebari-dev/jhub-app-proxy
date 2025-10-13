// Package config provides application configuration with CLI flag parsing
package config

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Config holds application configuration
type Config struct {
	// Authentication
	AuthType string // "oauth", "none"

	// Process
	Command     []string
	DestPort    int
	CondaEnv    string
	WorkDir     string
	ForceAlive  bool
	StripPrefix bool // Strip service prefix before forwarding (default: true for most apps)

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

// NewFromFlags creates a Config from command line flags using cobra
// Returns the cobra command and config, or error
func NewFromFlags(version, buildTime string) (*cobra.Command, *Config, error) {
	cfg := &Config{}

	rootCmd := &cobra.Command{
		Use:     "jhub-app-proxy [flags] -- command [args...]",
		Short:   "Process spawner with OAuth2 authentication for JupyterHub apps",
		Version: fmt.Sprintf("%s (built %s)", version, buildTime),
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
			return nil
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

	// Prefix handling (default: strip prefix like jhsingle-native-proxy)
	rootCmd.Flags().BoolVar(&cfg.StripPrefix, "strip-prefix", true,
		"Strip service prefix before forwarding to backend (default: true, use false for JupyterLab)")

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

	return rootCmd, cfg, nil
}

// NormalizePort handles backward compatibility and environment variable loading
func (c *Config) NormalizePort() {
	// Handle backward compatibility: --listen-port → --port
	if c.Port == 0 && c.ListenPort != 0 {
		c.Port = c.ListenPort
	}
	// Try to get port from environment variable (JupyterHub sets this)
	if c.Port == 0 {
		if envPort := os.Getenv("JHUB_APPS_SPAWNER_PORT"); envPort != "" {
			fmt.Sscanf(envPort, "%d", &c.Port)
		}
	}
	// Default port if still not set
	if c.Port == 0 {
		c.Port = 8888
	}
}
