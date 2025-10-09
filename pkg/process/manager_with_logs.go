// Package process - Manager extensions for log capture and exposure
package process

import (
	"context"
	"time"

	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
)

// ManagerWithLogs extends Manager with log capture capabilities
type ManagerWithLogs struct {
	*Manager
	logBuffer *LogBuffer
}

// NewManagerWithLogs creates a process manager with log capture
func NewManagerWithLogs(cfg Config, logCfg LogCaptureConfig, log *logger.Logger) (*ManagerWithLogs, error) {
	var logBuffer *LogBuffer

	// Create log buffer if enabled
	if logCfg.Enabled {
		logBuffer = NewLogBuffer(logCfg.BufferSize)

		// Store original handler
		originalHandler := cfg.OutputHandler

		// Override output handler to capture logs
		cfg.OutputHandler = func(stream string, line string) {
			// Capture to buffer (with PID placeholder, will be set after start)
			logBuffer.Append(LogEntry{
				Timestamp: time.Now(),
				Stream:    stream,
				Line:      line,
				PID:       0, // Will be updated by manager
			})

			// Call original handler if exists
			if originalHandler != nil {
				originalHandler(stream, line)
			}
		}
	}

	// Create base manager
	mgr, err := NewManager(cfg, log)
	if err != nil {
		return nil, err
	}

	return &ManagerWithLogs{
		Manager:   mgr,
		logBuffer: logBuffer,
	}, nil
}

// AddErrorLog adds an error message directly to the log buffer
// Useful for startup errors that occur before process output pipes are created
func (m *ManagerWithLogs) AddErrorLog(message string) {
	if m.logBuffer != nil {
		m.logBuffer.Append(LogEntry{
			Timestamp: time.Now(),
			Stream:    "stderr",
			Line:      message,
			PID:       m.GetPID(),
		})
	}
}

// GetRecentLogs returns the most recent N log entries
// Returns empty slice if log capture is disabled
func (m *ManagerWithLogs) GetRecentLogs(n int) []LogEntry {
	if m.logBuffer == nil {
		return []LogEntry{}
	}
	entries := m.logBuffer.GetRecent(n)
	// Update PIDs
	pid := m.GetPID()
	for i := range entries {
		entries[i].PID = pid
	}
	return entries
}

// GetLogsSince returns all logs since the given timestamp
func (m *ManagerWithLogs) GetLogsSince(since time.Time) []LogEntry {
	if m.logBuffer == nil {
		return []LogEntry{}
	}
	entries := m.logBuffer.GetSince(since)
	// Update PIDs
	pid := m.GetPID()
	for i := range entries {
		entries[i].PID = pid
	}
	return entries
}

// GetLogsByStream returns recent logs filtered by stream (stdout/stderr)
func (m *ManagerWithLogs) GetLogsByStream(stream string, n int) []LogEntry {
	if m.logBuffer == nil {
		return []LogEntry{}
	}
	entries := m.logBuffer.GetByStream(stream, n)
	// Update PIDs
	pid := m.GetPID()
	for i := range entries {
		entries[i].PID = pid
	}
	return entries
}

// GetLogStats returns statistics about captured logs
func (m *ManagerWithLogs) GetLogStats() LogStats {
	if m.logBuffer == nil {
		return LogStats{}
	}
	return m.logBuffer.GetStats()
}

// GetLogsJSON returns logs in JSON format for API responses
func (m *ManagerWithLogs) GetLogsJSON(n int) ([]byte, error) {
	if m.logBuffer == nil {
		return []byte(`{"logs":[],"stats":{"enabled":false}}`), nil
	}
	return m.logBuffer.ToJSON(n)
}

// ClearLogs clears the log buffer
func (m *ManagerWithLogs) ClearLogs() {
	if m.logBuffer != nil {
		m.logBuffer.Clear()
	}
}

// StreamLogs returns a channel that streams new log entries in real-time
// Useful for WebSocket implementations or real-time log tailing
func (m *ManagerWithLogs) StreamLogs(ctx context.Context) <-chan LogEntry {
	ch := make(chan LogEntry, 100)

	if m.logBuffer == nil {
		close(ch)
		return ch
	}

	// Get the current timestamp to only stream new logs
	startTime := time.Now()

	go func() {
		defer close(ch)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		lastCheck := startTime

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Get new logs since last check
				newLogs := m.GetLogsSince(lastCheck)
				for _, entry := range newLogs {
					select {
					case ch <- entry:
						if entry.Timestamp.After(lastCheck) {
							lastCheck = entry.Timestamp
						}
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return ch
}

// GetAllLogsFromFile returns all logs from the persistent file
func (m *ManagerWithLogs) GetAllLogsFromFile() ([]string, error) {
	if m.logBuffer == nil {
		return nil, nil
	}
	return m.logBuffer.GetAllFromFile()
}

// GetLogFilePath returns the path to the persistent log file
func (m *ManagerWithLogs) GetLogFilePath() string {
	if m.logBuffer == nil {
		return ""
	}
	return m.logBuffer.GetLogFilePath()
}

// CloseLogFile closes and cleans up the log file
func (m *ManagerWithLogs) CloseLogFile() error {
	if m.logBuffer != nil {
		return m.logBuffer.Close()
	}
	return nil
}
