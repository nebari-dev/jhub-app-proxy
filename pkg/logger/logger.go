// Package logger provides production-grade structured logging using Go's standard library
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/lmittmann/tint"
)

// Level represents log levels
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Format represents log output formats
type Format string

const (
	FormatJSON   Format = "json"
	FormatPretty Format = "pretty"
)

// Config holds logging configuration with sensible defaults
type Config struct {
	Level      Level  // Log level (debug, info, warn, error)
	Format     Format // Output format (json, pretty)
	Output     io.Writer
	ShowCaller bool // Include file:line in logs
	TimeFormat string
}

// DefaultConfig returns production-ready logging configuration
func DefaultConfig() Config {
	return Config{
		Level:      LevelInfo,
		Format:     FormatJSON,
		Output:     os.Stdout,
		ShowCaller: false,
		TimeFormat: time.RFC3339,
	}
}

// Logger wraps slog.Logger with domain-specific logging methods
type Logger struct {
	logger *slog.Logger
}

// New creates a new production-ready structured logger
func New(cfg Config) *Logger {
	// Parse log level
	level := parseLevel(cfg.Level)

	// Configure output writer
	output := cfg.Output
	if output == nil {
		output = os.Stdout
	}

	var handler slog.Handler

	// Create handler based on format
	if cfg.Format == FormatPretty {
		// Use tint for colored output (always enabled, works even when piped)
		timeFormat := cfg.TimeFormat
		if timeFormat == "" {
			timeFormat = "2006-01-02 15:04:05.000"
		}

		handler = tint.NewHandler(output, &tint.Options{
			Level:      level,
			TimeFormat: timeFormat,
			NoColor:    false, // Always use colors
			AddSource:  cfg.ShowCaller,
		})
	} else {
		// JSON format for production
		opts := &slog.HandlerOptions{
			Level: level,
		}
		if cfg.ShowCaller {
			opts.AddSource = true
		}
		handler = slog.NewJSONHandler(output, opts)
	}

	logger := slog.New(handler).With("service", "jhub-app-proxy")

	return &Logger{
		logger: logger,
	}
}

// WithComponent creates a child logger with component context for modularity
func (l *Logger) WithComponent(component string) *Logger {
	return &Logger{
		logger: l.logger.With("component", component),
	}
}

// WithProcess creates a child logger with process context
func (l *Logger) WithProcess(pid int, command string) *Logger {
	return &Logger{
		logger: l.logger.With("pid", pid, "command", command),
	}
}

// WithFramework creates a child logger with framework context
func (l *Logger) WithFramework(framework string) *Logger {
	return &Logger{
		logger: l.logger.With("framework", framework),
	}
}

// WithUser creates a child logger with user context for request tracing
func (l *Logger) WithUser(username string) *Logger {
	return &Logger{
		logger: l.logger.With("user", username),
	}
}

// WithFields creates a child logger with arbitrary context fields
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	args := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	return &Logger{
		logger: l.logger.With(args...),
	}
}

// Debug logs debug level message with optional key-value pairs
func (l *Logger) Debug(msg string, keysAndValues ...interface{}) {
	l.logWithFields(slog.LevelDebug, msg, keysAndValues...)
}

// Info logs info level message with optional key-value pairs
func (l *Logger) Info(msg string, keysAndValues ...interface{}) {
	l.logWithFields(slog.LevelInfo, msg, keysAndValues...)
}

// Warn logs warning level message with optional key-value pairs
func (l *Logger) Warn(msg string, keysAndValues ...interface{}) {
	l.logWithFields(slog.LevelWarn, msg, keysAndValues...)
}

// Error logs error level message with error and optional key-value pairs
func (l *Logger) Error(msg string, err error, keysAndValues ...interface{}) {
	if err != nil {
		keysAndValues = append([]interface{}{"error", err.Error(), "error_type", fmt.Sprintf("%T", err)}, keysAndValues...)
	}
	l.logWithFields(slog.LevelError, msg, keysAndValues...)
}

// Fatal logs fatal level message with error and exits with code 1
func (l *Logger) Fatal(msg string, err error, keysAndValues ...interface{}) {
	if err != nil {
		keysAndValues = append([]interface{}{"error", err.Error(), "error_type", fmt.Sprintf("%T", err)}, keysAndValues...)
	}
	l.logWithFields(slog.LevelError, msg, keysAndValues...)
	os.Exit(1)
}

// ProcessOutput logs subprocess output with proper stream designation
func (l *Logger) ProcessOutput(stream string, line string) {
	l.logger.Info("subprocess output", "stream", stream, "output", line)
}

// ProcessFailed logs subprocess failure with comprehensive error context
func (l *Logger) ProcessFailed(exitCode int, stderr, stdout string, err error) {
	args := []any{"exit_code", exitCode, "stderr", stderr, "stdout", stdout}
	if err != nil {
		args = append(args, "error", err.Error())
	}
	l.logger.Error("subprocess failed", args...)
}

// ProcessStarted logs process start with full command visibility
func (l *Logger) ProcessStarted(pid int, command []string, env map[string]string) {
	l.logger.Info("process started", "pid", pid, "command", command, "env", env)
}

// ProcessExited logs process exit with duration and exit code
func (l *Logger) ProcessExited(pid int, exitCode int, duration time.Duration) {
	msg := "process exited successfully"
	if exitCode != 0 {
		msg = "process exited with error"
	}
	l.logger.Info(msg, "pid", pid, "exit_code", exitCode, "duration", duration)
}

// Progress logs progress updates for long-running operations
func (l *Logger) Progress(stage string, keysAndValues ...interface{}) {
	keysAndValues = append([]interface{}{"stage", stage}, keysAndValues...)
	l.logWithFields(slog.LevelInfo, "progress", keysAndValues...)
}

// Metric logs metrics for monitoring and observability
func (l *Logger) Metric(name string, value interface{}, keysAndValues ...interface{}) {
	keysAndValues = append([]interface{}{"metric", name, "value", value}, keysAndValues...)
	l.logWithFields(slog.LevelInfo, "metric recorded", keysAndValues...)
}

// HealthCheck logs health check attempts with comprehensive context
func (l *Logger) HealthCheck(attempt, maxAttempts int, url string, success bool, latency time.Duration, err error) {
	msg := "health check succeeded"
	if !success {
		msg = "health check failed"
	}
	args := []any{"attempt", attempt, "max_attempts", maxAttempts, "url", url, "success", success, "latency", latency}
	if err != nil {
		args = append(args, "error", err.Error())
	}
	l.logger.Info(msg, args...)
}

// StartupBanner logs a concise startup message with configuration
func (l *Logger) StartupBanner(version string, config map[string]interface{}) {
	l.logger.Info("jhub-app-proxy starting", "version", version, "config", config)
}

// ShutdownBanner logs a clear shutdown message
func (l *Logger) ShutdownBanner(reason string) {
	l.logger.Info("==================================================")
	l.logger.Info("Shutting down JHub Apps Proxy", "reason", reason)
	l.logger.Info("==================================================")
}

// HubAPICall logs JupyterHub API calls for debugging auth and activity reporting
func (l *Logger) HubAPICall(method, endpoint string, statusCode int, duration time.Duration, err error) {
	msg := "Hub API call succeeded"
	args := []any{"method", method, "endpoint", endpoint, "status_code", statusCode, "duration", duration}
	if err != nil {
		msg = "Hub API call failed"
		args = append(args, "error", err.Error())
	}
	l.logger.Info(msg, args...)
}

// GitOperation logs git clone/pull operations with progress visibility
func (l *Logger) GitOperation(operation, repo, branch, dest string, err error) {
	msg := "git operation succeeded"
	args := []any{"operation", operation, "repo", repo, "branch", branch, "destination", dest}
	if err != nil {
		msg = "git operation failed"
		args = append(args, "error", err.Error())
	}
	l.logger.Info(msg, args...)
}

// CondaActivation logs conda environment activation attempts
func (l *Logger) CondaActivation(envName, envPath string, err error) {
	msg := "conda environment activated"
	args := []any{"env_name", envName, "env_path", envPath}
	if err != nil {
		msg = "conda activation failed"
		args = append(args, "error", err.Error())
	}
	l.logger.Info(msg, args...)
}

// logWithFields is a helper to add key-value pairs to log events
func (l *Logger) logWithFields(level slog.Level, msg string, keysAndValues ...interface{}) {
	if len(keysAndValues)%2 != 0 {
		l.logger.Warn("odd number of key-value pairs provided to logger", "args_count", len(keysAndValues))
		keysAndValues = append(keysAndValues, "<missing_value>")
	}

	l.logger.Log(context.Background(), level, msg, keysAndValues...)
}

// GetSlog returns the underlying slog.Logger for advanced use cases
func (l *Logger) GetSlog() *slog.Logger {
	return l.logger
}

// parseLevel converts string level to slog.Level
func parseLevel(level Level) slog.Level {
	switch level {
	case LevelDebug:
		return slog.LevelDebug
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
