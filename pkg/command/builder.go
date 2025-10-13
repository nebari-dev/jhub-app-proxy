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
	logger *logger.Logger
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
		var err error
		command, err = condaMgr.BuildActivationCommand(condaEnv, command)
		if err != nil {
			return nil, fmt.Errorf("failed to build conda activation: %w", err)
		}
	}

	return command, nil
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
