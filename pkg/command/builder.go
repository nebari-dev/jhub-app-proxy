// Package command provides command building and manipulation utilities
package command

import (
	"fmt"
	"os"
	"strings"

	"github.com/nebari-dev/jhub-app-proxy/pkg/conda"
	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
)

// Builder helps construct and manipulate commands for subprocess execution
type Builder struct {
	logger         *logger.Logger
	condaWarning   string // Stores conda activation warning if any
}

// NewBuilder creates a new command builder
func NewBuilder(log *logger.Logger) *Builder {
	return &Builder{
		logger: log,
	}
}

// Build constructs the final command with conda activation if needed
func (b *Builder) Build(command []string, condaEnv string) ([]string, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("no command specified")
	}

	// Apply conda activation if specified
	if condaEnv != "" {
		condaMgr := conda.NewManager(b.logger)
		activatedCommand, err := condaMgr.BuildActivationCommand(condaEnv, command)
		if err != nil {
			// Store warning message for later display in interim UI
			b.condaWarning = fmt.Sprintf("WARNING: Conda environment activation failed: %s. Running command without conda activation.", err.Error())

			// Log warning but continue with original command without conda activation
			b.logger.Warn("conda environment activation failed, running command without conda activation",
				"conda_env", condaEnv,
				"error", err.Error())
			// Return original command without conda activation
			return command, nil
		}
		command = activatedCommand
	}

	return command, nil
}

// GetCondaWarning returns the conda activation warning message if any
func (b *Builder) GetCondaWarning() string {
	return b.condaWarning
}

// SubstitutePort replaces jhsingle-native-proxy style placeholders in command arguments
// Handles: {port} → actual port, {-} → -, {--} → --, and strips surrounding quotes
func SubstitutePort(command []string, allocatedPort int) []string {
	result := make([]string, len(command))
	portStr := fmt.Sprintf("%d", allocatedPort)

	for i, arg := range command {
		processed := arg

		// Replace port placeholder
		processed = strings.ReplaceAll(processed, "{port}", portStr)

		// Replace dash placeholders (jhsingle-native-proxy compatibility)
		processed = strings.ReplaceAll(processed, "{-}", "-")
		processed = strings.ReplaceAll(processed, "{--}", "--")

		// Strip surrounding single quotes if present
		if len(processed) >= 2 && processed[0] == '\'' && processed[len(processed)-1] == '\'' {
			processed = processed[1 : len(processed)-1]
		}

		// Strip surrounding double quotes if present
		if len(processed) >= 2 && processed[0] == '"' && processed[len(processed)-1] == '"' {
			processed = processed[1 : len(processed)-1]
		}

		result[i] = processed
	}

	return result
}

// BuildEnv creates environment variables map for the subprocess
// Passes through JupyterHub environment variables
func BuildEnv() map[string]string {
	env := make(map[string]string)

	// Pass through JupyterHub environment variables
	jupyterHubEnvVars := []string{
		"JUPYTERHUB_API_TOKEN",
		"JUPYTERHUB_API_URL",
		"JUPYTERHUB_BASE_URL",
		"JUPYTERHUB_USER",
		"JUPYTERHUB_SERVER_NAME",
		"JUPYTERHUB_SERVICE_PREFIX",
		"JUPYTERHUB_GROUP",
	}

	for _, key := range jupyterHubEnvVars {
		if val := os.Getenv(key); val != "" {
			env[key] = val
		}
	}

	return env
}
