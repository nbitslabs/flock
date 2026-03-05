package server

import (
	"fmt"
	"net/http"
	"sync"
)

// SSEClient represents a connected SSE client.
type SSEClient struct {
	id       string
	messages chan string
	done     chan struct{}
}

// SSEBroker manages SSE client connections and event broadcasting.
type SSEBroker struct {
	mu      sync.RWMutex
	clients map[string]*SSEClient
}

func NewSSEBroker() *SSEBroker {
	return &SSEBroker{
		clients: make(map[string]*SSEClient),
	}
}

// ServeHTTP handles SSE connections for a specific session.
func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request, sessionID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	client := &SSEClient{
		id:       sessionID + "-" + r.RemoteAddr,
		messages: make(chan string, 64),
		done:     make(chan struct{}),
	}

	b.mu.Lock()
	b.clients[client.id] = client
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.clients, client.id)
		b.mu.Unlock()
		close(client.done)
	}()

	// Send initial connected event
	fmt.Fprintf(w, "event: connected\ndata: {\"sessionId\":%q}\n\n", sessionID)
	flusher.Flush()

	for {
		select {
		case msg := <-client.messages:
			fmt.Fprint(w, msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// Broadcast sends an event to all connected clients.
func (b *SSEBroker) Broadcast(instanceID, eventType, data string) {
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, data)

	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, client := range b.clients {
		select {
		case client.messages <- msg:
		default:
			// Drop message if client buffer is full
		}
	}
}
