package command

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
	"github.com/nebari-dev/jhub-app-proxy/pkg/pixi"
)

func TestGetRootPath(t *testing.T) {
	tests := []struct {
		name          string
		servicePrefix string
		expected      string
	}{
		{
			name:          "standard service prefix",
			servicePrefix: "/user/fakeuser/myapp/",
			expected:      "/hub/user/fakeuser/myapp",
		},
		{
			name:          "service prefix without trailing slash",
			servicePrefix: "/user/testuser/app",
			expected:      "/hub/user/testuser/app",
		},
		{
			name:          "service prefix without leading slash",
			servicePrefix: "user/demouser/app/",
			expected:      "/hub/user/demouser/app",
		},
		{
			name:          "empty service prefix",
			servicePrefix: "",
			expected:      "",
		},
		{
			name:          "simple service prefix",
			servicePrefix: "/user/alice/",
			expected:      "/hub/user/alice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.servicePrefix != "" {
				os.Setenv("JUPYTERHUB_SERVICE_PREFIX", tt.servicePrefix)
			} else {
				os.Unsetenv("JUPYTERHUB_SERVICE_PREFIX")
			}
			defer os.Unsetenv("JUPYTERHUB_SERVICE_PREFIX")

			result := GetRootPath()
			if result != tt.expected {
				t.Errorf("GetRootPath() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestSubstitutePort(t *testing.T) {
	tests := []struct {
		name          string
		command       []string
		port          int
		servicePrefix string
		expected      []string
	}{
		{
			name:          "substitute port only",
			command:       []string{"python", "-m", "http.server", "{port}"},
			port:          8080,
			servicePrefix: "",
			expected:      []string{"python", "-m", "http.server", "8080"},
		},
		{
			name:          "substitute root_path only",
			command:       []string{"myapp", "--root-path", "{root_path}"},
			port:          8080,
			servicePrefix: "/user/test/app/",
			expected:      []string{"myapp", "--root-path", "/hub/user/test/app"},
		},
		{
			name:          "substitute both port and root_path",
			command:       []string{"myapp", "--port", "{port}", "--root-path", "{root_path}"},
			port:          9000,
			servicePrefix: "/user/bob/dashboard/",
			expected:      []string{"myapp", "--port", "9000", "--root-path", "/hub/user/bob/dashboard"},
		},
		{
			name:          "substitute dash placeholders",
			command:       []string{"myapp", "{-}p", "{port}", "{--}root-path", "{root_path}"},
			port:          8888,
			servicePrefix: "/user/test/",
			expected:      []string{"myapp", "-p", "8888", "--root-path", "/hub/user/test"},
		},
		{
			name:          "strip single quotes",
			command:       []string{"'myapp --port {port}'"},
			port:          3000,
			servicePrefix: "",
			expected:      []string{"myapp --port 3000"},
		},
		{
			name:          "strip double quotes",
			command:       []string{`"myapp --root-path {root_path}"`},
			port:          3000,
			servicePrefix: "/user/demo/",
			expected:      []string{"myapp --root-path /hub/user/demo"},
		},
		{
			name:          "empty root_path when no service prefix",
			command:       []string{"myapp", "--root-path", "{root_path}"},
			port:          5000,
			servicePrefix: "",
			expected:      []string{"myapp", "--root-path", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.servicePrefix != "" {
				os.Setenv("JUPYTERHUB_SERVICE_PREFIX", tt.servicePrefix)
			} else {
				os.Unsetenv("JUPYTERHUB_SERVICE_PREFIX")
			}
			defer os.Unsetenv("JUPYTERHUB_SERVICE_PREFIX")

			result := SubstitutePort(tt.command, tt.port)
			if len(result) != len(tt.expected) {
				t.Fatalf("SubstitutePort() returned %d args, want %d", len(result), len(tt.expected))
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("SubstitutePort()[%d] = %q, want %q", i, result[i], tt.expected[i])
				}
			}
		})
	}
}

// newTestLogger creates a logger suitable for testing.
func newTestLogger() *logger.Logger {
	return logger.New(logger.Config{
		Level:  logger.LevelDebug,
		Format: logger.FormatJSON,
		Output: os.Stderr,
	})
}

// setupPixiTestEnv creates fake nebi and pixi binaries plus a workspace with a
// pixi.toml manifest. It sets PATH so both binaries are discoverable and returns
// the environment name to pass to Build().
func setupPixiTestEnv(t *testing.T) (tmpDir string, envName string) {
	t.Helper()
	tmpDir = t.TempDir()
	workspacePath := filepath.Join(tmpDir, "workspaces", "data-science")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "pixi.toml"), []byte("[project]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	workspaces := []pixi.Workspace{
		{Name: "data-science", Path: workspacePath, Missing: false},
	}
	wsJSON, _ := json.Marshal(workspaces)

	nebiBin := filepath.Join(tmpDir, "nebi")
	if err := os.WriteFile(nebiBin, []byte("#!/bin/sh\necho '"+string(wsJSON)+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	pixiBin := filepath.Join(tmpDir, "pixi")
	if err := os.WriteFile(pixiBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	return tmpDir, "alice/data-science"
}

func TestBuild_PixiWithValidEnv(t *testing.T) {
	_, envName := setupPixiTestEnv(t)
	t.Setenv("JHUB_APP_ENV_MANAGER", "pixi")

	b := NewBuilder(newTestLogger())
	cmd, err := b.Build([]string{"python", "app.py"}, envName)
	if err != nil {
		t.Fatalf("Build() error = %v, want nil", err)
	}

	// Command should start with "pixi run"
	if len(cmd) < 2 || cmd[0] != "pixi" || cmd[1] != "run" {
		t.Fatalf("Build() = %v, want command starting with [pixi run ...]", cmd)
	}

	// Should contain the original command at the end
	if cmd[len(cmd)-2] != "python" || cmd[len(cmd)-1] != "app.py" {
		t.Errorf("Build() command tail = %v, want [python app.py]", cmd[len(cmd)-2:])
	}

	// No pixi warning should be stored
	if w := b.GetPixiWarning(); w != "" {
		t.Errorf("GetPixiWarning() = %q, want empty", w)
	}
}

func TestBuild_PixiWithFailingEnv(t *testing.T) {
	// Set up nebi that returns workspaces, but request a non-existent one
	tmpDir := t.TempDir()
	workspaces := []pixi.Workspace{
		{Name: "other", Path: "/tmp/other", Missing: false},
	}
	wsJSON, _ := json.Marshal(workspaces)

	nebiBin := filepath.Join(tmpDir, "nebi")
	if err := os.WriteFile(nebiBin, []byte("#!/bin/sh\necho '"+string(wsJSON)+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pixiBin := filepath.Join(tmpDir, "pixi")
	if err := os.WriteFile(pixiBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)
	t.Setenv("JHUB_APP_ENV_MANAGER", "pixi")

	b := NewBuilder(newTestLogger())
	cmd, err := b.Build([]string{"python", "app.py"}, "alice/nonexistent")
	if err != nil {
		t.Fatalf("Build() error = %v, want nil (graceful fallback)", err)
	}

	// Should return the original command unchanged
	if len(cmd) != 2 || cmd[0] != "python" || cmd[1] != "app.py" {
		t.Errorf("Build() = %v, want [python app.py]", cmd)
	}

	// A pixi warning should be stored
	if w := b.GetPixiWarning(); w == "" {
		t.Error("GetPixiWarning() = empty, want warning about failed activation")
	} else if !strings.Contains(w, "WARNING") {
		t.Errorf("GetPixiWarning() = %q, want it to contain 'WARNING'", w)
	}
}

func TestBuild_DefaultCondaPath(t *testing.T) {
	// Unset the env manager so it falls through to conda
	t.Setenv("JHUB_APP_ENV_MANAGER", "")

	b := NewBuilder(newTestLogger())
	cmd, err := b.Build([]string{"python", "app.py"}, "myenv")
	if err != nil {
		t.Fatalf("Build() error = %v, want nil (graceful fallback)", err)
	}

	// Conda will fail (no conda on PATH in test), so original command returned
	if len(cmd) != 2 || cmd[0] != "python" || cmd[1] != "app.py" {
		t.Errorf("Build() = %v, want [python app.py]", cmd)
	}

	// A conda warning should be stored
	if w := b.GetCondaWarning(); w == "" {
		t.Error("GetCondaWarning() = empty, want warning about failed activation")
	}
}

func TestBuild_PixiNotOnPath(t *testing.T) {
	// Set up nebi with a valid workspace, but do NOT put pixi on PATH.
	// Use only tmpDir in PATH so the real system pixi is not discoverable.
	tmpDir := t.TempDir()
	workspacePath := filepath.Join(tmpDir, "workspaces", "data-science")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "pixi.toml"), []byte("[project]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	workspaces := []pixi.Workspace{
		{Name: "data-science", Path: workspacePath, Missing: false},
	}
	wsJSON, _ := json.Marshal(workspaces)

	nebiBin := filepath.Join(tmpDir, "nebi")
	if err := os.WriteFile(nebiBin, []byte("#!/bin/sh\necho '"+string(wsJSON)+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Intentionally do NOT create a pixi binary

	// Set PATH to only tmpDir so nebi is found but pixi is not
	t.Setenv("PATH", tmpDir)
	t.Setenv("JHUB_APP_ENV_MANAGER", "pixi")

	b := NewBuilder(newTestLogger())
	cmd, err := b.Build([]string{"python", "app.py"}, "alice/data-science")
	if err != nil {
		t.Fatalf("Build() error = %v, want nil (graceful fallback)", err)
	}

	// Should return original command unchanged
	if len(cmd) != 2 || cmd[0] != "python" || cmd[1] != "app.py" {
		t.Errorf("Build() = %v, want [python app.py]", cmd)
	}

	// A pixi warning about pixi not found should be stored
	if w := b.GetPixiWarning(); w == "" {
		t.Error("GetPixiWarning() = empty, want warning about pixi not found")
	} else if !strings.Contains(w, "pixi not found in PATH") {
		t.Errorf("GetPixiWarning() = %q, want it to contain 'pixi not found in PATH'", w)
	}
}
