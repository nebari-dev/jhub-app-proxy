// Package api provides HTTP API endpoints for log exposure
package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/nebari-dev/jhub-app-proxy/pkg/logger"
	"github.com/nebari-dev/jhub-app-proxy/pkg/process"
	"github.com/nebari-dev/jhub-app-proxy/pkg/ui"
)

var (
	// Version information (set by main package)
	Version string
)

// LogsHandler provides HTTP endpoints for accessing subprocess logs
// This allows jhub-apps to surface logs to users
type LogsHandler struct {
	manager *process.ManagerWithLogs
	logger  *logger.Logger
}

// NewLogsHandler creates a new logs API handler
func NewLogsHandler(manager *process.ManagerWithLogs, log *logger.Logger) *LogsHandler {
	return &LogsHandler{
		manager: manager,
		logger:  log.WithComponent("logs-api"),
	}
}

// HandleGetLogs returns recent logs
// GET /api/logs?lines=100&stream=stdout
func (h *LogsHandler) HandleGetLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	linesStr := r.URL.Query().Get("lines")
	lines := 100 // default
	if linesStr != "" {
		if n, err := strconv.Atoi(linesStr); err == nil && n > 0 {
			lines = n
			if lines > 10000 {
				lines = 10000 // cap at 10k lines for safety
			}
		}
	}

	stream := r.URL.Query().Get("stream") // "stdout", "stderr", or "" for all

	var entries []process.LogEntry
	if stream != "" && (stream == "stdout" || stream == "stderr") {
		entries = h.manager.GetLogsByStream(stream, lines)
	} else {
		entries = h.manager.GetRecentLogs(lines)
	}

	stats := h.manager.GetLogStats()

	response := map[string]interface{}{
		"logs":  entries,
		"stats": stats,
		"query": map[string]interface{}{
			"lines":  lines,
			"stream": stream,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("failed to encode logs response", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	h.logger.Debug("logs retrieved",
		"lines_requested", lines,
		"lines_returned", len(entries),
		"stream", stream)
}

// HandleGetLogsSince returns logs since a specific timestamp
// GET /api/logs/since?timestamp=2025-01-15T10:30:00Z
func (h *LogsHandler) HandleGetLogsSince(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	timestampStr := r.URL.Query().Get("timestamp")
	if timestampStr == "" {
		http.Error(w, "timestamp parameter required", http.StatusBadRequest)
		return
	}

	timestamp, err := time.Parse(time.RFC3339, timestampStr)
	if err != nil {
		http.Error(w, "invalid timestamp format (use RFC3339)", http.StatusBadRequest)
		return
	}

	entries := h.manager.GetLogsSince(timestamp)

	response := map[string]interface{}{
		"logs":  entries,
		"since": timestamp,
		"count": len(entries),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("failed to encode logs response", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// HandleGetStats returns log buffer statistics
// GET /api/logs/stats
func (h *LogsHandler) HandleGetStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := h.manager.GetLogStats()
	processState := map[string]interface{}{
		"state":   string(h.manager.GetState()),
		"pid":     h.manager.GetPID(),
		"uptime":  h.manager.GetUptime().Seconds(),
		"running": h.manager.IsRunning(),
	}

	processInfo := map[string]interface{}{
		"command": h.manager.GetCommand(),
		"workdir": h.manager.GetWorkDir(),
	}

	response := map[string]interface{}{
		"logs_stats":    stats,
		"process_state": processState,
		"process_info":  processInfo,
		"version":       Version,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("failed to encode stats response", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// HandleClearLogs clears the log buffer
// DELETE /api/logs
func (h *LogsHandler) HandleClearLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.manager.ClearLogs()
	h.logger.Info("logs cleared via API")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "logs cleared",
	})
}

// HandleGetAllLogs returns all logs from the persistent file
// GET /api/logs/all
func (h *LogsHandler) HandleGetAllLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lines, err := h.manager.GetAllLogsFromFile()
	if err != nil {
		h.logger.Error("failed to read logs from file", err)
		http.Error(w, "Failed to read logs", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"logs":       lines,
		"count":      len(lines),
		"source":     "file",
		"log_file":   h.manager.GetLogFilePath(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("failed to encode logs response", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// HandleGetLogo returns the logo as base64-encoded PNG
// GET /api/logo
func (h *LogsHandler) HandleGetLogo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logoBase64 := base64.StdEncoding.EncodeToString(ui.LogoPNG)

	response := map[string]interface{}{
		"logo": logoBase64,
		"type": "image/png",
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("failed to encode logo response", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// RegisterRoutes registers all log API routes with a http.ServeMux
func (h *LogsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/logs", h.HandleGetLogs)
	mux.HandleFunc("/api/logs/all", h.HandleGetAllLogs)
	mux.HandleFunc("/api/logs/since", h.HandleGetLogsSince)
	mux.HandleFunc("/api/logs/stats", h.HandleGetStats)
	mux.HandleFunc("/api/logs/clear", h.HandleClearLogs)
	mux.HandleFunc("/api/logo", h.HandleGetLogo)

	h.logger.Info("log API routes registered",
		"endpoints", []string{
			"GET /api/logs",
			"GET /api/logs/all",
			"GET /api/logs/since",
			"GET /api/logs/stats",
			"DELETE /api/logs/clear",
			"GET /api/logo",
		})
}
