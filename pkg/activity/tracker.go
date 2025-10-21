// Package activity provides activity tracking for JupyterHub activity reporting
package activity

import (
	"sync"
	"time"
)

// Tracker records the last activity timestamp in a thread-safe manner
// Used to track when the proxied application was last accessed for reporting to JupyterHub
type Tracker struct {
	mu           sync.RWMutex
	lastActivity *time.Time
}

// NewTracker creates a new activity tracker
func NewTracker() *Tracker {
	return &Tracker{}
}

// RecordActivity records the current time as the last activity timestamp
// This should be called on every HTTP request to the proxied application
func (t *Tracker) RecordActivity() {
	now := time.Now().UTC()
	t.mu.Lock()
	t.lastActivity = &now
	t.mu.Unlock()
}

// GetLastActivity returns the last recorded activity timestamp
// Returns nil if no activity has been recorded yet
func (t *Tracker) GetLastActivity() *time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.lastActivity
}
