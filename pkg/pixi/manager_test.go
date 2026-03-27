package pixi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
)

// newTestLogger creates a logger suitable for testing (writes to stderr so
// test failures include log context, using JSON format for machine parsing).
func newTestLogger() *logger.Logger {
	return logger.New(logger.Config{
		Level:  logger.LevelDebug,
		Format: logger.FormatJSON,
		Output: os.Stderr,
	})
}

// --- FindWorkspacePath tests ---

func TestFindWorkspacePath_Success(t *testing.T) {
	// Create a fake nebi script that outputs a known workspace list
	tmpDir := t.TempDir()
	workspacePath := filepath.Join(tmpDir, "workspaces", "data-science")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}

	workspaces := []Workspace{
		{Name: "data-science", Path: workspacePath, Missing: false},
		{Name: "ml-tools", Path: "/tmp/ml-tools", Missing: false},
	}
	wsJSON, _ := json.Marshal(workspaces)

	nebiBin := filepath.Join(tmpDir, "nebi")
	script := "#!/bin/sh\necho '" + string(wsJSON) + "'\n"
	if err := os.WriteFile(nebiBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	// Put our fake nebi on PATH
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	mgr := NewManager(newTestLogger())
	got, err := mgr.FindWorkspacePath("alice/data-science")
	if err != nil {
		t.Fatalf("FindWorkspacePath() error = %v, want nil", err)
	}
	if got != workspacePath {
		t.Errorf("FindWorkspacePath() = %q, want %q", got, workspacePath)
	}
}

func TestFindWorkspacePath_NebiNotFound(t *testing.T) {
	// Ensure nebi is not on PATH by setting PATH to empty temp dir
	tmpDir := t.TempDir()
	t.Setenv("PATH", tmpDir)

	mgr := NewManager(newTestLogger())
	_, err := mgr.FindWorkspacePath("alice/data-science")
	if err == nil {
		t.Fatal("FindWorkspacePath() error = nil, want error about nebi not found")
	}
	if got := err.Error(); !contains(got, "nebi not found in PATH") {
		t.Errorf("error = %q, want it to contain 'nebi not found in PATH'", got)
	}
}

func TestFindWorkspacePath_NebiError(t *testing.T) {
	// Create a nebi script that exits with error
	tmpDir := t.TempDir()
	nebiBin := filepath.Join(tmpDir, "nebi")
	script := "#!/bin/sh\necho 'nebi: connection refused' >&2\nexit 1\n"
	if err := os.WriteFile(nebiBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	mgr := NewManager(newTestLogger())
	_, err := mgr.FindWorkspacePath("alice/data-science")
	if err == nil {
		t.Fatal("FindWorkspacePath() error = nil, want error about nebi command failure")
	}
	if got := err.Error(); !contains(got, "failed to run") {
		t.Errorf("error = %q, want it to contain 'failed to run'", got)
	}
}

func TestFindWorkspacePath_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	nebiBin := filepath.Join(tmpDir, "nebi")
	script := "#!/bin/sh\necho 'this is not json'\n"
	if err := os.WriteFile(nebiBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	mgr := NewManager(newTestLogger())
	_, err := mgr.FindWorkspacePath("alice/data-science")
	if err == nil {
		t.Fatal("FindWorkspacePath() error = nil, want JSON parse error")
	}
	if got := err.Error(); !contains(got, "failed to parse nebi workspace list JSON") {
		t.Errorf("error = %q, want it to contain 'failed to parse nebi workspace list JSON'", got)
	}
}

func TestFindWorkspacePath_WorkspaceNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	workspaces := []Workspace{
		{Name: "other-env", Path: "/tmp/other-env", Missing: false},
	}
	wsJSON, _ := json.Marshal(workspaces)

	nebiBin := filepath.Join(tmpDir, "nebi")
	script := "#!/bin/sh\necho '" + string(wsJSON) + "'\n"
	if err := os.WriteFile(nebiBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	mgr := NewManager(newTestLogger())
	_, err := mgr.FindWorkspacePath("alice/data-science")
	if err == nil {
		t.Fatal("FindWorkspacePath() error = nil, want workspace not found error")
	}
	if got := err.Error(); !contains(got, "not found in nebi workspace list") {
		t.Errorf("error = %q, want it to contain 'not found in nebi workspace list'", got)
	}
}

func TestFindWorkspacePath_EmptyList(t *testing.T) {
	tmpDir := t.TempDir()
	nebiBin := filepath.Join(tmpDir, "nebi")
	script := "#!/bin/sh\necho '[]'\n"
	if err := os.WriteFile(nebiBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	mgr := NewManager(newTestLogger())
	_, err := mgr.FindWorkspacePath("alice/data-science")
	if err == nil {
		t.Fatal("FindWorkspacePath() error = nil, want empty list error")
	}
	if got := err.Error(); !contains(got, "no workspaces found") {
		t.Errorf("error = %q, want it to contain 'no workspaces found'", got)
	}
}

func TestFindWorkspacePath_MissingWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	workspaces := []Workspace{
		{Name: "data-science", Path: "/tmp/data-science", Missing: true},
	}
	wsJSON, _ := json.Marshal(workspaces)

	nebiBin := filepath.Join(tmpDir, "nebi")
	script := "#!/bin/sh\necho '" + string(wsJSON) + "'\n"
	if err := os.WriteFile(nebiBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	mgr := NewManager(newTestLogger())
	_, err := mgr.FindWorkspacePath("alice/data-science")
	if err == nil {
		t.Fatal("FindWorkspacePath() error = nil, want missing workspace error")
	}
	if got := err.Error(); !contains(got, "marked as missing") {
		t.Errorf("error = %q, want it to contain 'marked as missing'", got)
	}
}

func TestFindWorkspacePath_NameWithoutOwner(t *testing.T) {
	// Test that envName without "/" prefix is handled (uses full string as workspace name)
	tmpDir := t.TempDir()
	workspacePath := filepath.Join(tmpDir, "workspaces", "my-env")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}

	workspaces := []Workspace{
		{Name: "my-env", Path: workspacePath, Missing: false},
	}
	wsJSON, _ := json.Marshal(workspaces)

	nebiBin := filepath.Join(tmpDir, "nebi")
	script := "#!/bin/sh\necho '" + string(wsJSON) + "'\n"
	if err := os.WriteFile(nebiBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	mgr := NewManager(newTestLogger())
	got, err := mgr.FindWorkspacePath("my-env")
	if err != nil {
		t.Fatalf("FindWorkspacePath() error = %v, want nil", err)
	}
	if got != workspacePath {
		t.Errorf("FindWorkspacePath() = %q, want %q", got, workspacePath)
	}
}

// --- BuildActivationCommand tests ---

func TestBuildActivationCommand_WithPixiToml(t *testing.T) {
	tmpDir := t.TempDir()
	workspacePath := filepath.Join(tmpDir, "workspaces", "data-science")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create pixi.toml
	if err := os.WriteFile(filepath.Join(workspacePath, "pixi.toml"), []byte("[project]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	workspaces := []Workspace{
		{Name: "data-science", Path: workspacePath, Missing: false},
	}
	wsJSON, _ := json.Marshal(workspaces)

	nebiBin := filepath.Join(tmpDir, "nebi")
	script := "#!/bin/sh\necho '" + string(wsJSON) + "'\n"
	if err := os.WriteFile(nebiBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	mgr := NewManager(newTestLogger())
	got, err := mgr.BuildActivationCommand("alice/data-science", []string{"python", "app.py"})
	if err != nil {
		t.Fatalf("BuildActivationCommand() error = %v, want nil", err)
	}

	expected := []string{
		"pixi", "run",
		"--manifest-path", filepath.Join(workspacePath, "pixi.toml"),
		"-e", "default",
		"--",
		"python", "app.py",
	}

	if len(got) != len(expected) {
		t.Fatalf("BuildActivationCommand() returned %d args, want %d\ngot:  %v\nwant: %v", len(got), len(expected), got, expected)
	}
	for i := range got {
		if got[i] != expected[i] {
			t.Errorf("BuildActivationCommand()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestBuildActivationCommand_WithPyprojectToml(t *testing.T) {
	tmpDir := t.TempDir()
	workspacePath := filepath.Join(tmpDir, "workspaces", "ml-tools")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create pyproject.toml (no pixi.toml)
	if err := os.WriteFile(filepath.Join(workspacePath, "pyproject.toml"), []byte("[project]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	workspaces := []Workspace{
		{Name: "ml-tools", Path: workspacePath, Missing: false},
	}
	wsJSON, _ := json.Marshal(workspaces)

	nebiBin := filepath.Join(tmpDir, "nebi")
	script := "#!/bin/sh\necho '" + string(wsJSON) + "'\n"
	if err := os.WriteFile(nebiBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	mgr := NewManager(newTestLogger())
	got, err := mgr.BuildActivationCommand("bob/ml-tools", []string{"streamlit", "run", "app.py"})
	if err != nil {
		t.Fatalf("BuildActivationCommand() error = %v, want nil", err)
	}

	expected := []string{
		"pixi", "run",
		"--manifest-path", filepath.Join(workspacePath, "pyproject.toml"),
		"-e", "default",
		"--",
		"streamlit", "run", "app.py",
	}

	if len(got) != len(expected) {
		t.Fatalf("BuildActivationCommand() returned %d args, want %d\ngot:  %v\nwant: %v", len(got), len(expected), got, expected)
	}
	for i := range got {
		if got[i] != expected[i] {
			t.Errorf("BuildActivationCommand()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestBuildActivationCommand_PixiTomlPreferredOverPyproject(t *testing.T) {
	// When both pixi.toml and pyproject.toml exist, pixi.toml should be preferred
	tmpDir := t.TempDir()
	workspacePath := filepath.Join(tmpDir, "workspaces", "both")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "pixi.toml"), []byte("[project]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "pyproject.toml"), []byte("[project]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	workspaces := []Workspace{
		{Name: "both", Path: workspacePath, Missing: false},
	}
	wsJSON, _ := json.Marshal(workspaces)

	nebiBin := filepath.Join(tmpDir, "nebi")
	script := "#!/bin/sh\necho '" + string(wsJSON) + "'\n"
	if err := os.WriteFile(nebiBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	mgr := NewManager(newTestLogger())
	got, err := mgr.BuildActivationCommand("user/both", []string{"python", "-c", "pass"})
	if err != nil {
		t.Fatalf("BuildActivationCommand() error = %v, want nil", err)
	}

	// Should use pixi.toml, not pyproject.toml
	expectedManifest := filepath.Join(workspacePath, "pixi.toml")
	if got[3] != expectedManifest {
		t.Errorf("manifest path = %q, want %q (pixi.toml should be preferred)", got[3], expectedManifest)
	}
}

func TestBuildActivationCommand_NoManifest(t *testing.T) {
	tmpDir := t.TempDir()
	workspacePath := filepath.Join(tmpDir, "workspaces", "bare")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}
	// No pixi.toml or pyproject.toml

	workspaces := []Workspace{
		{Name: "bare", Path: workspacePath, Missing: false},
	}
	wsJSON, _ := json.Marshal(workspaces)

	nebiBin := filepath.Join(tmpDir, "nebi")
	script := "#!/bin/sh\necho '" + string(wsJSON) + "'\n"
	if err := os.WriteFile(nebiBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	mgr := NewManager(newTestLogger())
	_, err := mgr.BuildActivationCommand("user/bare", []string{"python", "app.py"})
	if err == nil {
		t.Fatal("BuildActivationCommand() error = nil, want error about missing manifest")
	}
	if got := err.Error(); !contains(got, "no manifest file") {
		t.Errorf("error = %q, want it to contain 'no manifest file'", got)
	}
}

func TestBuildActivationCommand_EmptyCommand(t *testing.T) {
	mgr := NewManager(newTestLogger())
	_, err := mgr.BuildActivationCommand("alice/data-science", []string{})
	if err == nil {
		t.Fatal("BuildActivationCommand() error = nil, want error about empty command")
	}
	if got := err.Error(); !contains(got, "empty command") {
		t.Errorf("error = %q, want it to contain 'empty command'", got)
	}
}

func TestBuildActivationCommand_EmptyEnvName(t *testing.T) {
	mgr := NewManager(newTestLogger())
	cmd := []string{"python", "app.py"}
	got, err := mgr.BuildActivationCommand("", cmd)
	if err != nil {
		t.Fatalf("BuildActivationCommand() error = %v, want nil", err)
	}
	// Should return original command unchanged
	if len(got) != len(cmd) {
		t.Fatalf("BuildActivationCommand() returned %d args, want %d", len(got), len(cmd))
	}
	for i := range got {
		if got[i] != cmd[i] {
			t.Errorf("BuildActivationCommand()[%d] = %q, want %q", i, got[i], cmd[i])
		}
	}
}

func TestBuildActivationCommand_WorkspaceNotFound(t *testing.T) {
	// nebi returns workspaces but none match
	tmpDir := t.TempDir()
	workspaces := []Workspace{
		{Name: "other", Path: "/tmp/other", Missing: false},
	}
	wsJSON, _ := json.Marshal(workspaces)

	nebiBin := filepath.Join(tmpDir, "nebi")
	script := "#!/bin/sh\necho '" + string(wsJSON) + "'\n"
	if err := os.WriteFile(nebiBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	mgr := NewManager(newTestLogger())
	_, err := mgr.BuildActivationCommand("alice/nonexistent", []string{"python", "app.py"})
	if err == nil {
		t.Fatal("BuildActivationCommand() error = nil, want error about workspace not found")
	}
	if got := err.Error(); !contains(got, "not found") {
		t.Errorf("error = %q, want it to contain 'not found'", got)
	}
}

// --- ValidateEnvironment tests ---

func TestValidateEnvironment_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	workspacePath := filepath.Join(tmpDir, "workspaces", "data-science")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "pixi.toml"), []byte("[project]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	workspaces := []Workspace{
		{Name: "data-science", Path: workspacePath, Missing: false},
	}
	wsJSON, _ := json.Marshal(workspaces)

	nebiBin := filepath.Join(tmpDir, "nebi")
	script := "#!/bin/sh\necho '" + string(wsJSON) + "'\n"
	if err := os.WriteFile(nebiBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	mgr := NewManager(newTestLogger())
	err := mgr.ValidateEnvironment("alice/data-science")
	if err != nil {
		t.Fatalf("ValidateEnvironment() error = %v, want nil", err)
	}
}

func TestValidateEnvironment_MissingPath(t *testing.T) {
	// nebi returns a workspace whose path does not exist on disk
	tmpDir := t.TempDir()
	nonexistentPath := filepath.Join(tmpDir, "does-not-exist")

	workspaces := []Workspace{
		{Name: "data-science", Path: nonexistentPath, Missing: false},
	}
	wsJSON, _ := json.Marshal(workspaces)

	nebiBin := filepath.Join(tmpDir, "nebi")
	script := "#!/bin/sh\necho '" + string(wsJSON) + "'\n"
	if err := os.WriteFile(nebiBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	mgr := NewManager(newTestLogger())
	err := mgr.ValidateEnvironment("alice/data-science")
	if err == nil {
		t.Fatal("ValidateEnvironment() error = nil, want error about missing path")
	}
	if got := err.Error(); !contains(got, "workspace path does not exist") {
		t.Errorf("error = %q, want it to contain 'workspace path does not exist'", got)
	}
}

func TestValidateEnvironment_NoManifest(t *testing.T) {
	tmpDir := t.TempDir()
	workspacePath := filepath.Join(tmpDir, "workspaces", "bare")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}
	// No pixi.toml or pyproject.toml

	workspaces := []Workspace{
		{Name: "bare", Path: workspacePath, Missing: false},
	}
	wsJSON, _ := json.Marshal(workspaces)

	nebiBin := filepath.Join(tmpDir, "nebi")
	script := "#!/bin/sh\necho '" + string(wsJSON) + "'\n"
	if err := os.WriteFile(nebiBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	mgr := NewManager(newTestLogger())
	err := mgr.ValidateEnvironment("user/bare")
	if err == nil {
		t.Fatal("ValidateEnvironment() error = nil, want error about missing manifest")
	}
	if got := err.Error(); !contains(got, "no manifest file") {
		t.Errorf("error = %q, want it to contain 'no manifest file'", got)
	}
}

func TestValidateEnvironment_PathIsFile(t *testing.T) {
	// workspace path exists but is a file, not a directory
	tmpDir := t.TempDir()
	workspacePath := filepath.Join(tmpDir, "not-a-dir")
	if err := os.WriteFile(workspacePath, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}

	workspaces := []Workspace{
		{Name: "not-a-dir", Path: workspacePath, Missing: false},
	}
	wsJSON, _ := json.Marshal(workspaces)

	nebiBin := filepath.Join(tmpDir, "nebi")
	script := "#!/bin/sh\necho '" + string(wsJSON) + "'\n"
	if err := os.WriteFile(nebiBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	mgr := NewManager(newTestLogger())
	err := mgr.ValidateEnvironment("user/not-a-dir")
	if err == nil {
		t.Fatal("ValidateEnvironment() error = nil, want error about not being a directory")
	}
	if got := err.Error(); !contains(got, "not a directory") {
		t.Errorf("error = %q, want it to contain 'not a directory'", got)
	}
}

func TestValidateEnvironment_WithPyprojectToml(t *testing.T) {
	tmpDir := t.TempDir()
	workspacePath := filepath.Join(tmpDir, "workspaces", "pyproj")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "pyproject.toml"), []byte("[project]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	workspaces := []Workspace{
		{Name: "pyproj", Path: workspacePath, Missing: false},
	}
	wsJSON, _ := json.Marshal(workspaces)

	nebiBin := filepath.Join(tmpDir, "nebi")
	script := "#!/bin/sh\necho '" + string(wsJSON) + "'\n"
	if err := os.WriteFile(nebiBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	mgr := NewManager(newTestLogger())
	err := mgr.ValidateEnvironment("user/pyproj")
	if err != nil {
		t.Fatalf("ValidateEnvironment() error = %v, want nil", err)
	}
}

// contains is a helper to check substring presence
func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsSubstr(s, substr)
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
