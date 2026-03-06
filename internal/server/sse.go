package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
)

// SSEBroker manages per-session SSE connections to web UI clients.
type SSEBroker struct {
	mu sync.RWMutex
	// sessionID -> set of client channels
	clients map[string]map[chan string]struct{}
}

func NewSSEBroker() *SSEBroker {
	return &SSEBroker{
		clients: make(map[string]map[chan string]struct{}),
	}
}

func (b *SSEBroker) subscribe(sessionID string) chan string {
	ch := make(chan string, 128)
	b.mu.Lock()
	if b.clients[sessionID] == nil {
		b.clients[sessionID] = make(map[chan string]struct{})
	}
	b.clients[sessionID][ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *SSEBroker) unsubscribe(sessionID string, ch chan string) {
	b.mu.Lock()
	if subs, ok := b.clients[sessionID]; ok {
		delete(subs, ch)
		if len(subs) == 0 {
			delete(b.clients, sessionID)
		}
	}
	b.mu.Unlock()
}

func (b *SSEBroker) sendToSession(sessionID string, msg string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.clients[sessionID] {
		select {
		case ch <- msg:
		default:
			// drop if client is slow
		}
	}
}

// HandleEvent receives a raw JSON event from an OpenCode instance and
// routes it to the correct session's subscribers.
func (b *SSEBroker) HandleEvent(instanceID, rawJSON string) {
	// Parse just enough to extract session ID and event type
	var envelope struct {
		Type       string `json:"type"`
		Properties struct {
			SessionID string `json:"sessionID"`
			Info      struct {
				ID        string `json:"id"`
				SessionID string `json:"sessionID"`
			} `json:"info"`
			Part struct {
				SessionID string `json:"sessionID"`
			} `json:"part"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &envelope); err != nil {
		return
	}

	// Extract sessionID from whichever field has it
	sessionID := envelope.Properties.SessionID
	if sessionID == "" {
		sessionID = envelope.Properties.Info.SessionID
	}
	if sessionID == "" {
		sessionID = envelope.Properties.Part.SessionID
	}
	if sessionID == "" {
		// server-level events (heartbeat etc) — skip
		return
	}

	// Forward as SSE data line to matching session subscribers
	msg := fmt.Sprintf("data: %s\n\n", rawJSON)
	b.sendToSession(sessionID, msg)
}

// SubscribeInternal returns a read-only channel of raw SSE messages for a
// session and an unsubscribe function. Used by internal Go consumers (e.g.
// the agent orchestrator waiting for session.idle).
func (b *SSEBroker) SubscribeInternal(sessionID string) (<-chan string, func()) {
	ch := b.subscribe(sessionID)
	return ch, func() { b.unsubscribe(sessionID, ch) }
}

// ServeHTTP handles an SSE connection for a specific session.
func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request, sessionID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := b.subscribe(sessionID)
	defer b.unsubscribe(sessionID, ch)

	// Send connected event
	fmt.Fprintf(w, "data: {\"type\":\"connected\",\"properties\":{\"sessionID\":%q}}\n\n", sessionID)
	flusher.Flush()

	log.Printf("SSE client connected for session %s", sessionID[:min(len(sessionID), 12)])

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprint(w, msg)
			flusher.Flush()
		case <-r.Context().Done():
			log.Printf("SSE client disconnected for session %s", sessionID[:min(len(sessionID), 12)])
			return
		}
	}
}
