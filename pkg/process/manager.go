// Package process provides robust subprocess management with health monitoring,
// output streaming, and lifecycle management following SOLID principles.
package process

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
)

// ProcessState represents the current state of a managed process
type ProcessState string

const (
	StateInitializing ProcessState = "initializing"
	StateStarting     ProcessState = "starting"
	StateRunning      ProcessState = "running"
	StateFailed       ProcessState = "failed"
	StateStopped      ProcessState = "stopped"
)

// Config holds process configuration
type Config struct {
	Command       []string          // Command and arguments to execute
	Env           map[string]string // Additional environment variables
	WorkDir       string            // Working directory
	ReadyTimeout  time.Duration     // How long to wait for process to be ready
	ReadyCheck    ReadyChecker      // Function to check if process is ready
	OutputHandler OutputHandler     // Handler for process output
}

// ReadyChecker is a function type that checks if a process is ready
type ReadyChecker func(ctx context.Context) error

// OutputHandler processes subprocess output lines
type OutputHandler func(stream string, line string)

// Manager manages the lifecycle of a subprocess with production-grade features
type Manager struct {
	config Config
	logger *logger.Logger

	// Process state
	mu      sync.RWMutex
	cmd     *exec.Cmd
	state   ProcessState
	pid     int
	started time.Time
	stopped time.Time

	// Cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

// NewManager creates a new process manager with the given configuration
func NewManager(cfg Config, log *logger.Logger) (*Manager, error) {
	if len(cfg.Command) == 0 {
		return nil, fmt.Errorf("command cannot be empty")
	}

	if cfg.ReadyTimeout == 0 {
		cfg.ReadyTimeout = 5 * time.Minute
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		config: cfg,
		logger: log.WithComponent("process-manager"),
		state:  StateInitializing,
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

// Start starts the process and waits for it to be ready
// Returns an error if the process fails to start or ready check fails
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.state == StateRunning {
		m.mu.Unlock()
		return fmt.Errorf("process already running")
	}
	m.state = StateStarting
	m.mu.Unlock()

	m.logger.Progress("starting process", "command", m.config.Command)

	// Build command
	cmd := exec.CommandContext(m.ctx, m.config.Command[0], m.config.Command[1:]...)

	// Set working directory
	if m.config.WorkDir != "" {
		cmd.Dir = m.config.WorkDir
	}

	// Set environment
	cmd.Env = os.Environ()
	for k, v := range m.config.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Set process group so subprocess doesn't receive our signals
	// This allows parent to handle Ctrl+C gracefully
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Setup output pipes for streaming
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.setState(StateFailed)
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		m.setState(StateFailed)
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	m.started = time.Now()
	if err := cmd.Start(); err != nil {
		m.setState(StateFailed)
		m.logger.Error("failed to start process", err, "command", m.config.Command)
		return fmt.Errorf("failed to start process: %w", err)
	}

	m.mu.Lock()
	m.cmd = cmd
	m.pid = cmd.Process.Pid
	m.mu.Unlock()

	m.logger.ProcessStarted(m.pid, m.config.Command, m.config.Env)

	// Stream output in background
	var wg sync.WaitGroup
	wg.Add(2)
	go m.streamOutput(&wg, "stdout", stdout)
	go m.streamOutput(&wg, "stderr", stderr)

	// Wait for process to be ready (non-blocking - run in background)
	if m.config.ReadyCheck != nil {
		go func() {
			readyCtx, cancel := context.WithTimeout(ctx, m.config.ReadyTimeout)
			defer cancel()

			m.logger.Progress("waiting for process ready check",
				"pid", m.pid,
				"timeout", m.config.ReadyTimeout)

			if err := m.config.ReadyCheck(readyCtx); err != nil {
				m.setState(StateFailed)
				m.logger.Error("process ready check failed", err,
					"pid", m.pid,
					"timeout", m.config.ReadyTimeout)
				// Don't kill the process - let it run so logs are available
				// Users can see the error in the log viewer
			} else {
				m.setState(StateRunning)
				m.logger.Info("process ready check passed", "pid", m.pid)
			}
		}()
	} else {
		// No ready check, mark as running immediately
		m.setState(StateRunning)
	}
	m.logger.Info("process started successfully",
		"pid", m.pid,
		"startup_time", time.Since(m.started))

	// Monitor process in background
	go func() {
		defer wg.Wait() // Wait for output streams to finish
		if err := cmd.Wait(); err != nil {
			m.setState(StateFailed)
			exitCode := -1
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
			m.logger.ProcessExited(m.pid, exitCode, time.Since(m.started))
		} else {
			m.setState(StateStopped)
			m.logger.ProcessExited(m.pid, 0, time.Since(m.started))
		}
		m.stopped = time.Now()
	}()

	return nil
}

// Stop gracefully stops the process with SIGTERM, then SIGKILL if needed
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cmd == nil || m.cmd.Process == nil {
		return fmt.Errorf("no process to stop")
	}

	m.logger.Info("stopping process", "pid", m.pid)

	// Try graceful shutdown first (SIGTERM)
	if err := m.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Process might already be dead
		m.logger.Warn("failed to send SIGTERM", "pid", m.pid, "error", err)
	}

	// Wait a bit for graceful shutdown
	done := make(chan error, 1)
	go func() {
		done <- m.cmd.Wait()
	}()

	select {
	case <-time.After(10 * time.Second):
		// Force kill if not stopped gracefully
		m.logger.Warn("process did not stop gracefully, sending SIGKILL", "pid", m.pid)
		if err := m.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill process: %w", err)
		}
	case err := <-done:
		if err != nil {
			m.logger.Info("process stopped with error", "pid", m.pid, "error", err)
		} else {
			m.logger.Info("process stopped gracefully", "pid", m.pid)
		}
	}

	m.cancel() // Cancel context
	m.setState(StateStopped)
	return nil
}

// GetState returns the current process state (thread-safe)
func (m *Manager) GetState() ProcessState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// GetPID returns the process ID (thread-safe)
func (m *Manager) GetPID() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pid
}

// IsRunning returns true if the process is currently running
func (m *Manager) IsRunning() bool {
	return m.GetState() == StateRunning
}

// streamOutput reads from a pipe and logs each line
// This ensures all subprocess output is visible for debugging
func (m *Manager) streamOutput(wg *sync.WaitGroup, stream string, reader io.Reader) {
	defer wg.Done()

	scanner := bufio.NewScanner(reader)
	// Increase buffer size for long log lines
	const maxCapacity = 1024 * 1024 // 1MB
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	for scanner.Scan() {
		line := scanner.Text()

		// Log to structured logger
		m.logger.ProcessOutput(stream, line)

		// Call custom handler if provided
		if m.config.OutputHandler != nil {
			m.config.OutputHandler(stream, line)
		}
	}

	if err := scanner.Err(); err != nil {
		m.logger.Error("error reading process output", err, "stream", stream)
	}
}

// setState safely updates the process state
func (m *Manager) setState(state ProcessState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	oldState := m.state
	m.state = state
	m.logger.Debug("process state changed",
		"from", oldState,
		"to", state,
		"pid", m.pid)
}

// GetUptime returns how long the process has been running
func (m *Manager) GetUptime() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.started.IsZero() {
		return 0
	}

	if m.stopped.IsZero() {
		return time.Since(m.started)
	}

	return m.stopped.Sub(m.started)
}

// GetCommand returns the command being executed
func (m *Manager) GetCommand() []string {
	return m.config.Command
}

// GetWorkDir returns the working directory
func (m *Manager) GetWorkDir() string {
	return m.config.WorkDir
}
