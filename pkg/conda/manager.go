// Package conda provides conda environment activation support
package conda

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
)

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

// GetEnvPath returns the path to a conda environment
func (m *Manager) GetEnvPath(envName string) (string, error) {
	prefix, err := m.GetCondaPrefix()
	if err != nil {
		return "", err
	}

	// Try standard environment location
	envPath := filepath.Join(prefix, "envs", envName)
	if _, err := os.Stat(envPath); err == nil {
		return envPath, nil
	}

	// Check if envName is already a full path
	if filepath.IsAbs(envName) {
		if _, err := os.Stat(envName); err == nil {
			return envName, nil
		}
	}

	return "", fmt.Errorf("conda environment not found: %s", envName)
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
