package agent

import (
	"sync"
	"time"
)

// ActivityTracker records the last SSE event time per session,
// used to detect stale/stuck sessions.
type ActivityTracker struct {
	mu       sync.RWMutex
	activity map[string]time.Time // sessionID -> last event time
}

func NewActivityTracker() *ActivityTracker {
	return &ActivityTracker{
		activity: make(map[string]time.Time),
	}
}

// RecordActivity records that a session had activity now.
func (t *ActivityTracker) RecordActivity(sessionID string) {
	t.mu.Lock()
	t.activity[sessionID] = time.Now()
	t.mu.Unlock()
}

// LastActivity returns the last activity time for a session.
// Returns zero time if no activity recorded.
func (t *ActivityTracker) LastActivity(sessionID string) time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.activity[sessionID]
}

// Remove cleans up tracking for a session.
func (t *ActivityTracker) Remove(sessionID string) {
	t.mu.Lock()
	delete(t.activity, sessionID)
	t.mu.Unlock()
}
