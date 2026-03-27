// Package pixi provides pixi/nebi environment activation support
package pixi

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
)

// defaultPixiEnv is the pixi environment used for nebi workspaces.
// Nebi workspaces use the "default" pixi environment by convention.
const defaultPixiEnv = "default"

// Workspace represents a single entry from 'nebi workspace list --json'
type Workspace struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Missing bool   `json:"missing"`
}

// Manager handles pixi/nebi environment operations
type Manager struct {
	logger *logger.Logger
}

// NewManager creates a new pixi manager
func NewManager(log *logger.Logger) *Manager {
	return &Manager{
		logger: log.WithComponent("pixi-manager"),
	}
}

// FindWorkspacePath runs 'nebi workspace list --json' and returns the local
// filesystem path for the workspace matching envName. The envName is expected
// in the format "owner/workspace-name"; only the part after "/" is matched
// against the workspace list.
func (m *Manager) FindWorkspacePath(envName string) (string, error) {
	// Extract the workspace name from "owner/workspace-name" format
	workspaceName := envName
	if idx := strings.LastIndex(envName, "/"); idx >= 0 {
		workspaceName = envName[idx+1:]
	}
	m.logger.Debug("looking up workspace path",
		"env_name", envName,
		"workspace_name", workspaceName)

	// Find the nebi binary
	nebiPath, err := exec.LookPath("nebi")
	if err != nil {
		m.logger.Error("nebi binary not found in PATH", err,
			"env_name", envName)
		return "", fmt.Errorf("nebi not found in PATH: %w", err)
	}
	m.logger.Debug("found nebi binary", "nebi_path", nebiPath)

	// Run nebi workspace list --json
	cmd := exec.Command(nebiPath, "workspace", "list", "--json")
	output, err := cmd.Output()
	if err != nil {
		m.logger.Error("failed to run nebi workspace list", err,
			"env_name", envName,
			"nebi_path", nebiPath)
		return "", fmt.Errorf("failed to run 'nebi workspace list --json': %w", err)
	}
	m.logger.Debug("nebi workspace list output received",
		"output_length", len(output))

	// Parse JSON output
	var workspaces []Workspace
	if err := json.Unmarshal(output, &workspaces); err != nil {
		// Truncate raw output to avoid huge log entries from malformed nebi output
		rawOutput := string(output)
		const maxRawOutputLen = 500
		if len(rawOutput) > maxRawOutputLen {
			rawOutput = rawOutput[:maxRawOutputLen] + "... (truncated)"
		}
		m.logger.Error("failed to parse nebi workspace list JSON", err,
			"env_name", envName,
			"raw_output", rawOutput)
		return "", fmt.Errorf("failed to parse nebi workspace list JSON: %w", err)
	}
	m.logger.Debug("parsed workspace list",
		"num_workspaces", len(workspaces))

	if len(workspaces) == 0 {
		m.logger.Warn("nebi returned empty workspace list",
			"env_name", envName)
		return "", fmt.Errorf("no workspaces found (nebi returned empty list)")
	}

	// Search for matching workspace
	for _, ws := range workspaces {
		if ws.Name == workspaceName {
			if ws.Missing {
				m.logger.Warn("workspace found but marked as missing",
					"env_name", envName,
					"workspace_name", workspaceName,
					"workspace_path", ws.Path)
				return "", fmt.Errorf("workspace %q found but marked as missing at path %s", workspaceName, ws.Path)
			}
			m.logger.Info("workspace found",
				"env_name", envName,
				"workspace_name", workspaceName,
				"workspace_path", ws.Path)
			return ws.Path, nil
		}
	}

	// Workspace not found - log available workspaces for debugging
	available := make([]string, len(workspaces))
	for i, ws := range workspaces {
		available[i] = ws.Name
	}
	m.logger.Error("workspace not found in nebi workspace list", nil,
		"env_name", envName,
		"workspace_name", workspaceName,
		"available_workspaces", available)
	return "", fmt.Errorf("workspace %q not found in nebi workspace list (available: %s)", workspaceName, strings.Join(available, ", "))
}

// PullWorkspace runs 'nebi pull <workspace-name>' to sync the workspace from
// the remote nebi server.  The pod must already have the nebi binary,
// NEBI_REMOTE_URL, and NEBI_AUTH_TOKEN set.  Non-fatal: if pull fails the
// workspace may still exist locally from a previous sync.
func (m *Manager) PullWorkspace(envName string) {
	workspaceName := envName
	if idx := strings.LastIndex(envName, "/"); idx >= 0 {
		workspaceName = envName[idx+1:]
	}

	nebiPath, err := exec.LookPath("nebi")
	if err != nil {
		m.logger.Warn("nebi not found, skipping workspace pull",
			"env_name", envName, "error", err.Error())
		return
	}

	m.logger.Info("pulling workspace from remote nebi server",
		"env_name", envName, "workspace_name", workspaceName)

	cmd := exec.Command(nebiPath, "pull", workspaceName, "--force")
	output, err := cmd.CombinedOutput()
	if err != nil {
		m.logger.Error("nebi pull failed", err,
			"env_name", envName,
			"workspace_name", workspaceName,
			"output", string(output))
	} else {
		m.logger.Info("workspace pulled successfully",
			"env_name", envName,
			"workspace_name", workspaceName)
	}
}

// BuildActivationCommand creates a command that runs within a pixi environment.
// It pulls the workspace from the remote server, finds the local path, locates
// the manifest file (pixi.toml or pyproject.toml), and wraps the command with
// 'pixi run'.
func (m *Manager) BuildActivationCommand(envName string, command []string) ([]string, error) {
	if envName == "" {
		return command, nil
	}

	if len(command) == 0 {
		m.logger.Warn("empty command provided for pixi activation",
			"env_name", envName)
		return nil, fmt.Errorf("empty command provided for pixi activation")
	}

	// Pull the workspace from the remote nebi server before looking it up
	m.PullWorkspace(envName)

	// Find the workspace path via nebi
	workspacePath, err := m.FindWorkspacePath(envName)
	if err != nil {
		m.logger.Error("failed to find workspace path", err,
			"env_name", envName)
		return nil, err
	}

	// Verify pixi is available on PATH
	pixiPath, err := exec.LookPath("pixi")
	if err != nil {
		m.logger.Warn("pixi binary not found in PATH",
			"env_name", envName,
			"error", err.Error())
		return nil, fmt.Errorf("pixi not found in PATH: %w", err)
	}
	m.logger.Debug("found pixi binary", "pixi_path", pixiPath)

	// Find manifest file: check pixi.toml first, then pyproject.toml
	manifestPath := ""
	pixiToml := filepath.Join(workspacePath, "pixi.toml")
	pyprojectToml := filepath.Join(workspacePath, "pyproject.toml")

	if _, err := os.Stat(pixiToml); err == nil {
		manifestPath = pixiToml
		m.logger.Debug("found pixi.toml manifest",
			"env_name", envName,
			"manifest_path", manifestPath)
	} else if _, err := os.Stat(pyprojectToml); err == nil {
		manifestPath = pyprojectToml
		m.logger.Debug("found pyproject.toml manifest",
			"env_name", envName,
			"manifest_path", manifestPath)
	} else {
		m.logger.Error("no manifest file found in workspace", nil,
			"env_name", envName,
			"workspace_path", workspacePath,
			"checked_files", []string{"pixi.toml", "pyproject.toml"})
		return nil, fmt.Errorf("no manifest file (pixi.toml or pyproject.toml) found in workspace %s", workspacePath)
	}

	// Build: pixi run --manifest-path <path> -e default -- <command>
	activationCmd := []string{
		"pixi",
		"run",
		"--manifest-path", manifestPath,
		"-e", defaultPixiEnv,
		"--",
	}
	activationCmd = append(activationCmd, command...)

	m.logger.Info("pixi activation command built",
		"env_name", envName,
		"workspace_path", workspacePath,
		"manifest_path", manifestPath,
		"command", activationCmd)

	return activationCmd, nil
}

// ValidateEnvironment checks if a workspace exists and has a valid pixi manifest
func (m *Manager) ValidateEnvironment(envName string) error {
	workspacePath, err := m.FindWorkspacePath(envName)
	if err != nil {
		m.logger.Error("workspace validation failed: path not found", err,
			"env_name", envName)
		return fmt.Errorf("workspace validation failed: %w", err)
	}

	// Check if workspace path exists on disk
	info, err := os.Stat(workspacePath)
	if err != nil {
		m.logger.Error("workspace path does not exist on disk", err,
			"env_name", envName,
			"workspace_path", workspacePath)
		return fmt.Errorf("workspace path does not exist: %w", err)
	}
	if !info.IsDir() {
		m.logger.Error("workspace path is not a directory", nil,
			"env_name", envName,
			"workspace_path", workspacePath)
		return fmt.Errorf("workspace path is not a directory: %s", workspacePath)
	}

	// Check for manifest file
	pixiToml := filepath.Join(workspacePath, "pixi.toml")
	pyprojectToml := filepath.Join(workspacePath, "pyproject.toml")

	hasPixiToml := false
	hasPyprojectToml := false
	if _, err := os.Stat(pixiToml); err == nil {
		hasPixiToml = true
	}
	if _, err := os.Stat(pyprojectToml); err == nil {
		hasPyprojectToml = true
	}

	if !hasPixiToml && !hasPyprojectToml {
		m.logger.Error("no manifest file found in workspace", nil,
			"env_name", envName,
			"workspace_path", workspacePath)
		return fmt.Errorf("no manifest file (pixi.toml or pyproject.toml) found in workspace %s", workspacePath)
	}

	m.logger.Debug("workspace validated",
		"env_name", envName,
		"workspace_path", workspacePath,
		"has_pixi_toml", hasPixiToml,
		"has_pyproject_toml", hasPyprojectToml)

	return nil
}
