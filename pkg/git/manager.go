// Package git provides git repository cloning and management
package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
)

// Manager handles git operations
type Manager struct {
	logger *logger.Logger
}

// NewManager creates a new git manager
func NewManager(log *logger.Logger) *Manager {
	return &Manager{
		logger: log.WithComponent("git-manager"),
	}
}

// CloneConfig holds git clone configuration
type CloneConfig struct {
	RepoURL    string // Git repository URL
	Branch     string // Branch or tag to checkout
	DestPath   string // Destination path for the clone
	Depth      int    // Clone depth (0 for full clone, 1 for shallow)
	Submodules bool   // Whether to clone submodules
}

// Clone clones a git repository
func (m *Manager) Clone(cfg CloneConfig) error {
	m.logger.Progress("cloning git repository",
		"repo", cfg.RepoURL,
		"branch", cfg.Branch,
		"dest", cfg.DestPath)

	// Create destination directory if it doesn't exist
	if err := os.MkdirAll(cfg.DestPath, 0755); err != nil {
		m.logger.Error("failed to create destination directory", err,
			"dest", cfg.DestPath)
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Check if directory is already a git repo
	gitDir := filepath.Join(cfg.DestPath, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		m.logger.Info("git repository already exists, pulling latest changes",
			"dest", cfg.DestPath)
		return m.pull(cfg.DestPath, cfg.Branch)
	}

	// Build clone command
	args := []string{"clone"}

	// Add depth for shallow clone
	if cfg.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", cfg.Depth))
	}

	// Add branch
	if cfg.Branch != "" {
		args = append(args, "--branch", cfg.Branch)
	}

	// Add submodules flag
	if cfg.Submodules {
		args = append(args, "--recurse-submodules")
	}

	args = append(args, cfg.RepoURL, cfg.DestPath)

	// Execute clone
	cmd := exec.Command("git", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		m.logger.GitOperation("clone", cfg.RepoURL, cfg.Branch, cfg.DestPath, err)
		m.logger.Error("git clone failed", err,
			"output", string(output),
			"command", cmd.String())
		return fmt.Errorf("git clone failed: %w: %s", err, string(output))
	}

	m.logger.GitOperation("clone", cfg.RepoURL, cfg.Branch, cfg.DestPath, nil)
	m.logger.Info("git repository cloned successfully",
		"repo", cfg.RepoURL,
		"dest", cfg.DestPath)

	return nil
}

// pull updates an existing git repository
func (m *Manager) pull(repoPath string, branch string) error {
	m.logger.Progress("pulling git repository",
		"path", repoPath,
		"branch", branch)

	// Fetch latest changes
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = repoPath
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		m.logger.Error("git fetch failed", err, "output", string(output))
		return fmt.Errorf("git fetch failed: %w: %s", err, string(output))
	}

	// Checkout specified branch
	if branch != "" {
		checkoutCmd := exec.Command("git", "checkout", branch)
		checkoutCmd.Dir = repoPath
		if output, err := checkoutCmd.CombinedOutput(); err != nil {
			m.logger.Error("git checkout failed", err, "output", string(output))
			return fmt.Errorf("git checkout failed: %w: %s", err, string(output))
		}
	}

	// Pull latest changes
	pullCmd := exec.Command("git", "pull", "origin", branch)
	pullCmd.Dir = repoPath
	output, err := pullCmd.CombinedOutput()

	if err != nil {
		m.logger.GitOperation("pull", repoPath, branch, repoPath, err)
		m.logger.Error("git pull failed", err, "output", string(output))
		return fmt.Errorf("git pull failed: %w: %s", err, string(output))
	}

	m.logger.GitOperation("pull", repoPath, branch, repoPath, nil)
	m.logger.Info("git repository updated successfully", "path", repoPath)

	return nil
}

// IsGitInstalled checks if git is available
func (m *Manager) IsGitInstalled() bool {
	_, err := exec.LookPath("git")
	return err == nil
}
