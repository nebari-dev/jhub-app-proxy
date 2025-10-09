// Package logger provides production-grade structured logging with zero allocation
// and comprehensive error context tracking.
package logger

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
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

// Logger wraps zerolog with domain-specific logging methods
// Implements zero-allocation logging for production performance
type Logger struct {
	logger zerolog.Logger
}

// New creates a new production-ready structured logger
func New(cfg Config) *Logger {
	// Parse and set log level
	level := parseLevel(cfg.Level)
	zerolog.SetGlobalLevel(level)

	// Configure output writer
	output := cfg.Output
	if output == nil {
		output = os.Stdout
	}

	// Apply pretty formatting if requested (development mode)
	// Format: [info    2025-10-09 12:13:04.565 JHub App Proxy] message
	if cfg.Format == FormatPretty {
		timeFormat := cfg.TimeFormat
		if timeFormat == "" {
			timeFormat = "2006-01-02 15:04:05.000"
		}
		output = zerolog.ConsoleWriter{
			Out:        output,
			TimeFormat: timeFormat,
			NoColor:    false,
			FormatLevel: func(i interface{}) string {
				// Pad level to 7 chars (longest is "warning")
				level := fmt.Sprintf("%-7s", i)
				return fmt.Sprintf("[%s ", level)
			},
			FormatTimestamp: func(i interface{}) string {
				// Parse the timestamp and reformat it
				t, err := time.Parse(time.RFC3339, fmt.Sprintf("%s", i))
				if err != nil {
					// If parsing fails, use as-is
					return fmt.Sprintf("%s JHub App Proxy]", i)
				}
				// Format using our custom format
				formatted := t.Format(timeFormat)
				return fmt.Sprintf("%s JHub App Proxy]", formatted)
			},
			FormatMessage: func(i interface{}) string {
				return fmt.Sprintf(" %s", i)
			},
			FormatFieldName: func(i interface{}) string {
				return fmt.Sprintf(" %s=", i)
			},
			FormatFieldValue: func(i interface{}) string {
				return fmt.Sprintf("%s", i)
			},
			PartsOrder: []string{
				zerolog.LevelFieldName,
				zerolog.TimestampFieldName,
				zerolog.MessageFieldName,
			},
		}
	}

	// Build logger with context
	logger := zerolog.New(output).
		With().
		Timestamp().
		Str("service", "jhub-app-proxy")

	if cfg.ShowCaller {
		logger = logger.Caller()
	}

	return &Logger{
		logger: logger.Logger(),
	}
}

// WithComponent creates a child logger with component context for modularity
func (l *Logger) WithComponent(component string) *Logger {
	return &Logger{
		logger: l.logger.With().Str("component", component).Logger(),
	}
}

// WithProcess creates a child logger with process context
func (l *Logger) WithProcess(pid int, command string) *Logger {
	return &Logger{
		logger: l.logger.With().
			Int("pid", pid).
			Str("command", command).
			Logger(),
	}
}

// WithFramework creates a child logger with framework context
func (l *Logger) WithFramework(framework string) *Logger {
	return &Logger{
		logger: l.logger.With().Str("framework", framework).Logger(),
	}
}

// WithUser creates a child logger with user context for request tracing
func (l *Logger) WithUser(username string) *Logger {
	return &Logger{
		logger: l.logger.With().Str("user", username).Logger(),
	}
}

// WithFields creates a child logger with arbitrary context fields
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	ctx := l.logger.With()
	for k, v := range fields {
		ctx = ctx.Interface(k, v)
	}
	return &Logger{
		logger: ctx.Logger(),
	}
}

// Debug logs debug level message with optional key-value pairs
func (l *Logger) Debug(msg string, keysAndValues ...interface{}) {
	l.logWithFields(l.logger.Debug(), msg, keysAndValues...)
}

// Info logs info level message with optional key-value pairs
func (l *Logger) Info(msg string, keysAndValues ...interface{}) {
	l.logWithFields(l.logger.Info(), msg, keysAndValues...)
}

// Warn logs warning level message with optional key-value pairs
func (l *Logger) Warn(msg string, keysAndValues ...interface{}) {
	l.logWithFields(l.logger.Warn(), msg, keysAndValues...)
}

// Error logs error level message with error and optional key-value pairs
func (l *Logger) Error(msg string, err error, keysAndValues ...interface{}) {
	event := l.logger.Error()
	if err != nil {
		event = event.Err(err).
			Str("error_type", fmt.Sprintf("%T", err))
	}
	l.logWithFields(event, msg, keysAndValues...)
}

// Fatal logs fatal level message with error and exits with code 1
func (l *Logger) Fatal(msg string, err error, keysAndValues ...interface{}) {
	event := l.logger.Fatal()
	if err != nil {
		event = event.Err(err).
			Str("error_type", fmt.Sprintf("%T", err))
	}
	l.logWithFields(event, msg, keysAndValues...)
}

// ProcessOutput logs subprocess output with proper stream designation
// This ensures all subprocess output is visible and trackable
func (l *Logger) ProcessOutput(stream string, line string) {
	l.logger.Info().
		Str("stream", stream).
		Str("output", line).
		Msg("subprocess output")
}

// ProcessFailed logs subprocess failure with comprehensive error context
// Includes exit code, stderr, stdout for complete debugging visibility
func (l *Logger) ProcessFailed(exitCode int, stderr, stdout string, err error) {
	event := l.logger.Error().
		Int("exit_code", exitCode).
		Str("stderr", stderr).
		Str("stdout", stdout)

	if err != nil {
		event = event.Err(err)
	}

	event.Msg("subprocess failed")
}

// ProcessStarted logs process start with full command visibility
func (l *Logger) ProcessStarted(pid int, command []string, env map[string]string) {
	l.logger.Info().
		Int("pid", pid).
		Strs("command", command).
		Interface("env", env).
		Msg("process started")
}

// ProcessExited logs process exit with duration and exit code
func (l *Logger) ProcessExited(pid int, exitCode int, duration time.Duration) {
	event := l.logger.Info().
		Int("pid", pid).
		Int("exit_code", exitCode).
		Dur("duration", duration)

	if exitCode == 0 {
		event.Msg("process exited successfully")
	} else {
		event.Msg("process exited with error")
	}
}

// Progress logs progress updates for long-running operations
// Useful for tracking multi-step initialization processes
func (l *Logger) Progress(stage string, keysAndValues ...interface{}) {
	event := l.logger.Info().Str("stage", stage)
	l.logWithFields(event, "progress", keysAndValues...)
}

// Metric logs metrics for monitoring and observability
func (l *Logger) Metric(name string, value interface{}, keysAndValues ...interface{}) {
	event := l.logger.Info().
		Str("metric", name).
		Interface("value", value)
	l.logWithFields(event, "metric recorded", keysAndValues...)
}

// HealthCheck logs health check attempts with comprehensive context
// Shows attempt number, URL, success status, latency for debugging startup issues
func (l *Logger) HealthCheck(attempt, maxAttempts int, url string, success bool, latency time.Duration, err error) {
	event := l.logger.Info().
		Int("attempt", attempt).
		Int("max_attempts", maxAttempts).
		Str("url", url).
		Bool("success", success).
		Dur("latency", latency)

	if err != nil {
		event = event.Err(err)
	}

	if success {
		event.Msg("health check succeeded")
	} else {
		event.Msg("health check failed")
	}
}

// StartupBanner logs a concise startup message with configuration
func (l *Logger) StartupBanner(version string, config map[string]interface{}) {
	l.logger.Info().
		Str("version", version).
		Interface("config", config).
		Msg("jhub-app-proxy starting")
}

// ShutdownBanner logs a clear shutdown message
func (l *Logger) ShutdownBanner(reason string) {
	l.logger.Info().Msg("==================================================")
	l.logger.Info().
		Str("reason", reason).
		Msg("Shutting down JHub Apps Proxy")
	l.logger.Info().Msg("==================================================")
}

// HubAPICall logs JupyterHub API calls for debugging auth and activity reporting
func (l *Logger) HubAPICall(method, endpoint string, statusCode int, duration time.Duration, err error) {
	event := l.logger.Info().
		Str("method", method).
		Str("endpoint", endpoint).
		Int("status_code", statusCode).
		Dur("duration", duration)

	if err != nil {
		event.Err(err).Msg("Hub API call failed")
	} else {
		event.Msg("Hub API call succeeded")
	}
}

// GitOperation logs git clone/pull operations with progress visibility
func (l *Logger) GitOperation(operation, repo, branch, dest string, err error) {
	event := l.logger.Info().
		Str("operation", operation).
		Str("repo", repo).
		Str("branch", branch).
		Str("destination", dest)

	if err != nil {
		event.Err(err).Msg("git operation failed")
	} else {
		event.Msg("git operation succeeded")
	}
}

// CondaActivation logs conda environment activation attempts
func (l *Logger) CondaActivation(envName, envPath string, err error) {
	event := l.logger.Info().
		Str("env_name", envName).
		Str("env_path", envPath)

	if err != nil {
		event.Err(err).Msg("conda activation failed")
	} else {
		event.Msg("conda environment activated")
	}
}

// logWithFields is a helper to add key-value pairs to log events
// Validates even number of arguments for proper key-value pairing
func (l *Logger) logWithFields(event *zerolog.Event, msg string, keysAndValues ...interface{}) {
	if len(keysAndValues)%2 != 0 {
		l.logger.Warn().
			Int("args_count", len(keysAndValues)).
			Msg("odd number of key-value pairs provided to logger")
		keysAndValues = append(keysAndValues, "<missing_value>")
	}

	for i := 0; i < len(keysAndValues); i += 2 {
		key := fmt.Sprintf("%v", keysAndValues[i])
		event = event.Interface(key, keysAndValues[i+1])
	}

	event.Msg(msg)
}

// GetZerolog returns the underlying zerolog logger for advanced use cases
func (l *Logger) GetZerolog() zerolog.Logger {
	return l.logger
}

// parseLevel converts string level to zerolog.Level
func parseLevel(level Level) zerolog.Level {
	switch level {
	case LevelDebug:
		return zerolog.DebugLevel
	case LevelWarn:
		return zerolog.WarnLevel
	case LevelError:
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}
