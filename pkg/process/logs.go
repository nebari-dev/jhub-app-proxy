// Package process - Log capture and exposure functionality
package process

import (
	"bufio"
	"container/ring"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// LogEntry represents a single log line from the subprocess
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Stream    string    `json:"stream"` // "stdout" or "stderr"
	Line      string    `json:"line"`
	PID       int       `json:"pid"`
}

// LogBuffer is a thread-safe circular buffer for subprocess logs
// Keeps the most recent N log entries for user visibility
// Also writes all logs to a file for persistence
type LogBuffer struct {
	mu       sync.RWMutex
	buffer   *ring.Ring
	capacity int
	lines    int // Total lines captured (for stats)
	logFile  *os.File
	logPath  string
}

// NewLogBuffer creates a new log buffer with the specified capacity
// Creates a temporary file for persistent log storage
func NewLogBuffer(capacity int) *LogBuffer {
	if capacity <= 0 {
		capacity = 1000 // Default: keep last 1000 lines
	}

	// Create a temporary file for persistent logs
	logFile, err := os.CreateTemp("", "jhub-app-proxy-*.log")
	logPath := ""
	if err == nil {
		logPath = logFile.Name()
	}

	return &LogBuffer{
		buffer:   ring.New(capacity),
		capacity: capacity,
		logFile:  logFile,
		logPath:  logPath,
	}
}

// Append adds a new log entry to the buffer and writes to file
func (lb *LogBuffer) Append(entry LogEntry) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	// Add to memory buffer
	lb.buffer.Value = entry
	lb.buffer = lb.buffer.Next()
	lb.lines++

	// Write to persistent log file
	if lb.logFile != nil {
		// Format: [timestamp] [stream] line
		logLine := fmt.Sprintf("[%s] [%s] %s\n",
			entry.Timestamp.Format("2006-01-02 15:04:05.000"),
			entry.Stream,
			entry.Line)
		if _, err := lb.logFile.WriteString(logLine); err != nil {
			// Log write errors are logged but don't stop execution
			fmt.Fprintf(os.Stderr, "failed to write log to file: %v\n", err)
		}
		if err := lb.logFile.Sync(); err != nil {
			// Sync errors are logged but don't stop execution
			fmt.Fprintf(os.Stderr, "failed to sync log file: %v\n", err)
		}
	}
}

// GetRecent returns the most recent N log entries
// If n <= 0 or n > capacity, returns all available entries
func (lb *LogBuffer) GetRecent(n int) []LogEntry {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	if n <= 0 || n > lb.capacity {
		n = lb.capacity
	}

	// Calculate actual available entries
	available := lb.lines
	if available > lb.capacity {
		available = lb.capacity
	}

	if n > available {
		n = available
	}

	entries := make([]LogEntry, 0, n)

	// Start from the oldest entry in the ring
	start := lb.buffer
	if available < lb.capacity {
		// If we haven't filled the buffer yet, find the first non-nil entry
		for i := 0; i < lb.capacity; i++ {
			if start.Value == nil {
				start = start.Next()
			} else {
				break
			}
		}
	}

	// Collect entries
	current := start
	for i := 0; i < available; i++ {
		if current.Value != nil {
			entry, ok := current.Value.(LogEntry)
			if ok {
				entries = append(entries, entry)
			}
		}
		current = current.Next()
	}

	// Return only the last N entries
	if len(entries) > n {
		entries = entries[len(entries)-n:]
	}

	return entries
}

// GetSince returns all log entries since the given timestamp
func (lb *LogBuffer) GetSince(since time.Time) []LogEntry {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	entries := make([]LogEntry, 0)

	available := lb.lines
	if available > lb.capacity {
		available = lb.capacity
	}

	// Find the starting point
	start := lb.buffer
	if available < lb.capacity {
		for i := 0; i < lb.capacity; i++ {
			if start.Value == nil {
				start = start.Next()
			} else {
				break
			}
		}
	}

	// Collect entries after the timestamp
	current := start
	for i := 0; i < available; i++ {
		if current.Value != nil {
			entry, ok := current.Value.(LogEntry)
			if ok && entry.Timestamp.After(since) {
				entries = append(entries, entry)
			}
		}
		current = current.Next()
	}

	return entries
}

// GetByStream returns recent entries filtered by stream (stdout/stderr)
func (lb *LogBuffer) GetByStream(stream string, n int) []LogEntry {
	all := lb.GetRecent(-1) // Get all
	filtered := make([]LogEntry, 0)

	for _, entry := range all {
		if entry.Stream == stream {
			filtered = append(filtered, entry)
		}
	}

	// Return only the last N
	if n > 0 && len(filtered) > n {
		filtered = filtered[len(filtered)-n:]
	}

	return filtered
}

// Clear removes all entries from the buffer
func (lb *LogBuffer) Clear() {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.buffer = ring.New(lb.capacity)
	lb.lines = 0
}

// GetStats returns statistics about the log buffer
func (lb *LogBuffer) GetStats() LogStats {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	available := lb.lines
	if available > lb.capacity {
		available = lb.capacity
	}

	return LogStats{
		TotalLines:    lb.lines,
		BufferedLines: available,
		Capacity:      lb.capacity,
		BufferFull:    lb.lines >= lb.capacity,
	}
}

// LogStats represents statistics about the log buffer
type LogStats struct {
	TotalLines    int  `json:"total_lines"`    // Total lines captured (lifetime)
	BufferedLines int  `json:"buffered_lines"` // Currently buffered lines
	Capacity      int  `json:"capacity"`       // Buffer capacity
	BufferFull    bool `json:"buffer_full"`    // Whether buffer has wrapped
}

// ToJSON converts log entries to JSON for easy API responses
func (lb *LogBuffer) ToJSON(n int) ([]byte, error) {
	entries := lb.GetRecent(n)
	return json.Marshal(map[string]interface{}{
		"logs":  entries,
		"stats": lb.GetStats(),
	})
}

// GetAllFromFile reads all logs from the persistent file
// This allows retrieving logs even if they've been pushed out of the memory buffer
func (lb *LogBuffer) GetAllFromFile() ([]string, error) {
	lb.mu.RLock()
	logPath := lb.logPath
	lb.mu.RUnlock()

	if logPath == "" {
		return nil, fmt.Errorf("no log file available")
	}

	file, err := os.Open(logPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	return lines, scanner.Err()
}

// GetLogFilePath returns the path to the persistent log file
func (lb *LogBuffer) GetLogFilePath() string {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	return lb.logPath
}

// Close closes the log file and cleans up
func (lb *LogBuffer) Close() error {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if lb.logFile != nil {
		lb.logFile.Close()
		// Clean up the temporary file
		if lb.logPath != "" {
			os.Remove(lb.logPath)
		}
	}
	return nil
}

// LogCaptureConfig configures log capture behavior
type LogCaptureConfig struct {
	Enabled    bool // Enable log capture
	BufferSize int  // Number of log lines to keep in memory
}

// DefaultLogCaptureConfig returns sensible defaults
func DefaultLogCaptureConfig() LogCaptureConfig {
	return LogCaptureConfig{
		Enabled:    true,
		BufferSize: 1000,
	}
}
