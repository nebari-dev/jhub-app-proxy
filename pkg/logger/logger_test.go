package logger

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   string
	}{
		{
			name: "creates logger with default config",
			config: Config{
				Level:  LevelInfo,
				Format: FormatJSON,
			},
			want: "jhub-app-proxy",
		},
		{
			name: "creates logger with debug level",
			config: Config{
				Level:  LevelDebug,
				Format: FormatJSON,
			},
			want: "jhub-app-proxy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			tt.config.Output = buf

			logger := New(tt.config)
			if logger == nil {
				t.Fatal("expected logger to be non-nil")
			}

			logger.Info("test message")
			output := buf.String()

			if !strings.Contains(output, tt.want) {
				t.Errorf("expected output to contain %q, got %q", tt.want, output)
			}
		})
	}
}

func TestLoggerWithComponent(t *testing.T) {
	buf := &bytes.Buffer{}
	cfg := Config{
		Level:  LevelInfo,
		Format: FormatJSON,
		Output: buf,
	}

	logger := New(cfg)
	componentLogger := logger.WithComponent("test-component")
	componentLogger.Info("test message")

	output := buf.String()
	if !strings.Contains(output, "test-component") {
		t.Errorf("expected output to contain component name, got %q", output)
	}
}

func TestLoggerProcessOutput(t *testing.T) {
	buf := &bytes.Buffer{}
	cfg := Config{
		Level:  LevelInfo,
		Format: FormatJSON,
		Output: buf,
	}

	logger := New(cfg)
	logger.ProcessOutput("stdout", "test output line")

	output := buf.String()
	if !strings.Contains(output, "stdout") {
		t.Errorf("expected output to contain stream type, got %q", output)
	}
	if !strings.Contains(output, "test output line") {
		t.Errorf("expected output to contain log line, got %q", output)
	}
}

func TestLoggerProcessFailed(t *testing.T) {
	buf := &bytes.Buffer{}
	cfg := Config{
		Level:  LevelInfo,
		Format: FormatJSON,
		Output: buf,
	}

	logger := New(cfg)
	err := errors.New("process execution failed")
	logger.ProcessFailed(1, "error output", "standard output", err)

	output := buf.String()
	if !strings.Contains(output, "exit_code") {
		t.Errorf("expected output to contain exit code, got %q", output)
	}
	if !strings.Contains(output, "error output") {
		t.Errorf("expected output to contain stderr, got %q", output)
	}
}

func TestLoggerHealthCheck(t *testing.T) {
	buf := &bytes.Buffer{}
	cfg := Config{
		Level:  LevelInfo,
		Format: FormatJSON,
		Output: buf,
	}

	logger := New(cfg)
	logger.HealthCheck(1, 10, "http://localhost:8080/health", true, 50*time.Millisecond, nil)

	output := buf.String()
	if !strings.Contains(output, "attempt") {
		t.Errorf("expected output to contain attempt number, got %q", output)
	}
	if !strings.Contains(output, "success") {
		t.Errorf("expected output to contain success status, got %q", output)
	}
}

func TestLoggerWithFields(t *testing.T) {
	buf := &bytes.Buffer{}
	cfg := Config{
		Level:  LevelInfo,
		Format: FormatJSON,
		Output: buf,
	}

	logger := New(cfg)
	fieldsLogger := logger.WithFields(map[string]interface{}{
		"user_id": 123,
		"role":    "admin",
	})
	fieldsLogger.Info("user action")

	output := buf.String()
	if !strings.Contains(output, "123") {
		t.Errorf("expected output to contain user_id, got %q", output)
	}
	if !strings.Contains(output, "admin") {
		t.Errorf("expected output to contain role, got %q", output)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Level != LevelInfo {
		t.Errorf("expected level to be info, got %v", cfg.Level)
	}
	if cfg.Format != FormatJSON {
		t.Errorf("expected format to be json, got %v", cfg.Format)
	}
	if cfg.ShowCaller != false {
		t.Errorf("expected ShowCaller to be false, got %v", cfg.ShowCaller)
	}
}
