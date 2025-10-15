// Package conda provides conda environment activation support
package conda

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
)

// CondaInfo represents the structure returned by 'conda info --json'
type CondaInfo struct {
	CondaPrefix string   `json:"conda_prefix"`
	Envs        []string `json:"envs"`
}

// Manager handles conda environment operations
type Manager struct {
	logger *logger.Logger
}

// NewManager creates a new conda manager
func NewManager(log *logger.Logger) *Manager {
	return &Manager{
		logger: log.WithComponent("conda-manager"),
	}
}

// GetCondaPrefix returns the conda installation prefix
func (m *Manager) GetCondaPrefix() (string, error) {
	// Try CONDA_PREFIX env var first
	if prefix := os.Getenv("CONDA_PREFIX"); prefix != "" {
		return prefix, nil
	}

	// Try to find conda executable
	condaPath, err := exec.LookPath("conda")
	if err != nil {
		return "", fmt.Errorf("conda not found in PATH: %w", err)
	}

	// Get conda prefix from conda info
	cmd := exec.Command(condaPath, "info", "--base")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get conda base: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// GetCondaInfo returns conda information by calling 'conda info --json'
func (m *Manager) GetCondaInfo() (*CondaInfo, error) {
	condaExe := os.Getenv("CONDA_EXE")
	if condaExe == "" {
		condaExe = "conda"
	}

	m.logger.Debug("calling conda info", "conda_exe", condaExe)
	cmd := exec.Command(condaExe, "info", "--json")
	output, err := cmd.Output()
	if err != nil {
		m.logger.Warn("failed to run conda info", "error", err.Error())
		return nil, fmt.Errorf("failed to run conda info: %w", err)
	}

	var info CondaInfo
	if err := json.Unmarshal(output, &info); err != nil {
		m.logger.Warn("failed to parse conda info JSON", "error", err.Error())
		return nil, fmt.Errorf("failed to parse conda info JSON: %w", err)
	}

	m.logger.Debug("conda info retrieved",
		"conda_prefix", info.CondaPrefix,
		"num_envs", len(info.Envs))

	return &info, nil
}

// GetEnvPath returns the path to a conda environment
func (m *Manager) GetEnvPath(envName string) (string, error) {
	// Check if envName is already a full path
	if filepath.IsAbs(envName) {
		if _, err := os.Stat(envName); err == nil {
			m.logger.Info("using absolute path for conda environment", "env_path", envName)
			return envName, nil
		}
	}

	// Get conda info to find all environments
	condaInfo, err := m.GetCondaInfo()
	if err != nil {
		m.logger.Warn("failed to get conda info, falling back to standard location",
			"env_name", envName,
			"error", err.Error())
		// Fallback to standard location if conda info fails
		prefix, prefixErr := m.GetCondaPrefix()
		if prefixErr != nil {
			return "", fmt.Errorf("conda environment not found: %s (and failed to get conda prefix: %w)", envName, prefixErr)
		}
		envPath := filepath.Join(prefix, "envs", envName)
		if _, statErr := os.Stat(envPath); statErr == nil {
			m.logger.Info("found conda environment at fallback location", "env_path", envPath)
			return envPath, nil
		}
		return "", fmt.Errorf("conda environment not found: %s", envName)
	}

	// Default guess
	envPath := filepath.Join(condaInfo.CondaPrefix, "envs", envName)

	// Search through all environments by name
	for _, env := range condaInfo.Envs {
		lastName := filepath.Base(env)
		if lastName == envName {
			envPath = env
			m.logger.Debug("matched conda environment",
				"env_name", envName,
				"env_path", envPath)
			break
		}
	}

	// Verify the path exists
	if _, err := os.Stat(envPath); err != nil {
		m.logger.Error("conda environment path does not exist",
			err,
			"env_name", envName,
			"searched_path", envPath,
			"available_envs", len(condaInfo.Envs))
		return "", fmt.Errorf("conda environment not found: %s", envName)
	}

	m.logger.Info("found conda environment",
		"env_name", envName,
		"env_path", envPath)

	return envPath, nil
}

// BuildActivationCommand creates a command that activates a conda environment
// and runs the target command within it
func (m *Manager) BuildActivationCommand(envName string, command []string) ([]string, error) {
	if envName == "" {
		return command, nil
	}

	envPath, err := m.GetEnvPath(envName)
	if err != nil {
		m.logger.Error("failed to find conda environment", err, "env_name", envName)
		return nil, err
	}

	m.logger.Info("conda environment found", "env_name", envName, "env_path", envPath)

	// Build activation command
	// Use conda run to activate and execute in one go
	prefix, err := m.GetCondaPrefix()
	if err != nil {
		return nil, err
	}

	condaExec := filepath.Join(prefix, "bin", "conda")
	if _, err := os.Stat(condaExec); err != nil {
		// Fallback to conda in PATH
		condaExec = "conda"
	}

	// Build: conda run -p <env_path> <command>
	activationCmd := []string{
		condaExec,
		"run",
		"-p", envPath,
		"--no-capture-output", // Don't capture output (let us handle it)
	}

	activationCmd = append(activationCmd, command...)

	m.logger.CondaActivation(envName, envPath, nil)
	m.logger.Debug("conda activation command built",
		"env_name", envName,
		"command", activationCmd)

	return activationCmd, nil
}

// ValidateEnvironment checks if a conda environment exists and is valid
func (m *Manager) ValidateEnvironment(envName string) error {
	envPath, err := m.GetEnvPath(envName)
	if err != nil {
		return err
	}

	// Check if python exists in the environment
	pythonPath := filepath.Join(envPath, "bin", "python")
	if _, err := os.Stat(pythonPath); err != nil {
		return fmt.Errorf("python not found in conda environment %s: %w", envName, err)
	}

	m.logger.Debug("conda environment validated",
		"env_name", envName,
		"env_path", envPath,
		"python_path", pythonPath)

	return nil
}
