package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/nbitslabs/flock/internal/db/sqlc"
	"github.com/nbitslabs/flock/internal/memory"
)

// handleGetHeartbeat returns the HEARTBEAT.md for an instance.
func (s *Server) handleGetHeartbeat(w http.ResponseWriter, r *http.Request) {
	instanceID := r.PathValue("id")
	inst, ok := s.manager.Get(instanceID)
	if !ok {
		http.Error(w, "instance not found", http.StatusNotFound)
		return
	}

	content, err := memory.ReadHeartbeat(inst.WorkingDirectory)
	if err != nil {
		http.Error(w, "failed to read heartbeat: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/markdown")
	w.Write([]byte(content))
}

// handlePutHeartbeat updates the HEARTBEAT.md for an instance.
func (s *Server) handlePutHeartbeat(w http.ResponseWriter, r *http.Request) {
	instanceID := r.PathValue("id")
	inst, ok := s.manager.Get(instanceID)
	if !ok {
		http.Error(w, "instance not found", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	if err := memory.WriteHeartbeat(inst.WorkingDirectory, string(body)); err != nil {
		http.Error(w, "failed to write heartbeat: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleListTasks returns tasks for an instance.
func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	instanceID := r.PathValue("id")
	tasks, err := s.queries.ListTasksByInstance(r.Context(), instanceID)
	if err != nil {
		http.Error(w, "failed to list tasks: "+err.Error(), http.StatusInternalServerError)
		return
	}

	type taskResponse struct {
		ID             string `json:"id"`
		IssueNumber    int64  `json:"issue_number"`
		IssueURL       string `json:"issue_url"`
		Title          string `json:"title"`
		Status         string `json:"status"`
		SessionID      string `json:"session_id,omitempty"`
		BranchName     string `json:"branch_name,omitempty"`
		PRURL          string `json:"pr_url,omitempty"`
		LastActivityAt string `json:"last_activity_at"`
		CreatedAt      string `json:"created_at"`
	}

	result := make([]taskResponse, len(tasks))
	for i, t := range tasks {
		result[i] = taskResponse{
			ID:             t.ID,
			IssueNumber:    t.IssueNumber,
			IssueURL:       t.IssueUrl,
			Title:          t.Title,
			Status:         t.Status,
			SessionID:      t.SessionID,
			BranchName:     t.BranchName,
			PRURL:          t.PrUrl,
			LastActivityAt: t.LastActivityAt,
			CreatedAt:      t.CreatedAt,
		}
	}

	writeJSON(w, result)
}

// handleGetInstanceMemory returns .flock/memory/MEMORY.md for an instance.
func (s *Server) handleGetInstanceMemory(w http.ResponseWriter, r *http.Request) {
	instanceID := r.PathValue("id")
	inst, ok := s.manager.Get(instanceID)
	if !ok {
		http.Error(w, "instance not found", http.StatusNotFound)
		return
	}

	content, err := memory.ReadInstanceMemory(inst.WorkingDirectory)
	if err != nil {
		http.Error(w, "failed to read memory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/markdown")
	w.Write([]byte(content))
}

// handleGetGlobalMemory returns the global memory file.
func (s *Server) handleGetGlobalMemory(w http.ResponseWriter, r *http.Request) {
	content, err := memory.ReadGlobalMemory(s.dataDir)
	if err != nil {
		http.Error(w, "failed to read global memory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/markdown")
	w.Write([]byte(content))
}

// handlePutGlobalMemory updates the global memory file.
func (s *Server) handlePutGlobalMemory(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	if err := memory.WriteGlobalMemory(s.dataDir, string(body)); err != nil {
		http.Error(w, "failed to write global memory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleGetAgentStatus returns the agent harness status.
func (s *Server) handleGetAgentStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]any{
		"enabled": s.harness != nil && s.harness.Enabled(),
	}

	data, _ := json.Marshal(status)
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// ensureFlockAgentSession returns a valid flock agent session, creating a new
// one if the existing session no longer exists in OpenCode.
func (s *Server) ensureFlockAgentSession(ctx context.Context) (*sqlc.FlockAgentSession, error) {
	session, err := s.queries.GetActiveFlockAgentSession(ctx)
	if err == nil {
		// Verify the OpenCode session still exists
		if _, err := s.flockAgentClient.GetSession(ctx, session.SessionID); err == nil {
			return &session, nil
		}
		log.Printf("flock agent session %s no longer exists in opencode, recreating", session.SessionID[:12])
		s.queries.RetireFlockAgentSession(ctx, session.ID)
	}

	// Create a new session
	ocSession, err := s.flockAgentClient.CreateSession(ctx, s.dataDir)
	if err != nil {
		return nil, fmt.Errorf("create flock agent session: %w", err)
	}

	sessionID := ocSession.ID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	flockAgentID := uuid.New().String()
	newSession, err := s.queries.CreateFlockAgentSession(ctx, sqlc.CreateFlockAgentSessionParams{
		ID:               flockAgentID,
		SessionID:        sessionID,
		WorkingDirectory: s.dataDir,
	})
	if err != nil {
		return nil, fmt.Errorf("save flock agent session: %w", err)
	}

	log.Printf("created new flock agent session %s (opencode: %s)", flockAgentID[:8], sessionID[:12])
	return &newSession, nil
}

// handleGetFlockAgentSession returns the current Flock agent session.
func (s *Server) handleGetFlockAgentSession(w http.ResponseWriter, r *http.Request) {
	session, err := s.ensureFlockAgentSession(r.Context())
	if err != nil {
		writeJSON(w, map[string]any{"active": false})
		return
	}

	writeJSON(w, map[string]any{
		"active": true,
		"id":     session.ID,
		"status": session.Status,
	})
}

// handleCreateFlockAgentSession creates a new Flock agent session.
func (s *Server) handleCreateFlockAgentSession(w http.ResponseWriter, r *http.Request) {
	session, err := s.ensureFlockAgentSession(r.Context())
	if err != nil {
		log.Printf("failed to create flock agent session: %v", err)
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"id":         session.ID,
		"session_id": session.SessionID,
		"status":     session.Status,
	})
}

// handleRotateFlockAgentSession rotates (retires old and creates new) the Flock agent session.
func (s *Server) handleRotateFlockAgentSession(w http.ResponseWriter, r *http.Request) {
	activeSession, err := s.queries.GetActiveFlockAgentSession(r.Context())
	if err == nil && activeSession.Status == "active" {
		s.queries.RetireFlockAgentSession(r.Context(), activeSession.ID)
	}

	ocSession, err := s.flockAgentClient.CreateSession(r.Context(), s.dataDir)
	if err != nil {
		log.Printf("failed to create flock agent session: %v", err)
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	sessionID := ocSession.ID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	flockAgentID := uuid.New().String()
	_, err = s.queries.CreateFlockAgentSession(r.Context(), sqlc.CreateFlockAgentSessionParams{
		ID:               flockAgentID,
		SessionID:        sessionID,
		WorkingDirectory: s.dataDir,
	})
	if err != nil {
		log.Printf("failed to save flock agent session: %v", err)
		http.Error(w, "failed to save session", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"id":     flockAgentID,
		"session_id": sessionID,
		"status": "active",
	})
}

// handleGetFlockAgentMessages returns messages for the Flock agent session.
// Delegates to unified handler for DRY code.
func (s *Server) handleGetFlockAgentMessages(w http.ResponseWriter, r *http.Request) {
	session, err := s.ensureFlockAgentSession(r.Context())
	if err != nil {
		http.Error(w, "no active session", http.StatusNotFound)
		return
	}

	s.handleGetMessagesWithID(w, r, session.SessionID)
}

// handleSendFlockAgentMessage sends a message to the Flock agent session.
// Delegates to unified handler for DRY code.
func (s *Server) handleSendFlockAgentMessage(w http.ResponseWriter, r *http.Request) {
	session, err := s.ensureFlockAgentSession(r.Context())
	if err != nil {
		http.Error(w, "no active session", http.StatusInternalServerError)
		return
	}

	s.handleSendMessageWithID(w, r, session.SessionID)
}

// handleFlockAgentEvents returns SSE events for the Flock agent session.
// Delegates to unified handler for DRY code.
func (s *Server) handleFlockAgentEvents(w http.ResponseWriter, r *http.Request) {
	session, err := s.ensureFlockAgentSession(r.Context())
	if err != nil {
		http.Error(w, "no active session", http.StatusNotFound)
		return
	}

	s.handleSessionEventsWithID(w, r, session.SessionID)
}
