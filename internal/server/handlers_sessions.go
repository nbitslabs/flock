package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
)

type createSessionRequest struct {
	Title string `json:"title"`
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	instanceID := r.PathValue("id")
	// Verify instance exists
	if _, err := s.queries.GetInstance(r.Context(), instanceID); err != nil {
		http.Error(w, "instance not found", http.StatusNotFound)
		return
	}

	// Try to get sessions from OpenCode
	inst, ok := s.manager.Get(instanceID)
	if ok && inst.Client != nil {
		sessions, err := inst.Client.ListSessions(r.Context())
		if err == nil {
			writeJSON(w, sessions)
			return
		}
		log.Printf("failed to list sessions from opencode: %v, falling back to DB", err)
	}

	// Fallback to DB
	sessions, err := s.queries.ListSessionsByInstance(r.Context(), instanceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, sessions)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	instanceID := r.PathValue("id")
	inst, ok := s.manager.Get(instanceID)
	if !ok || inst.Client == nil {
		http.Error(w, "instance not available", http.StatusServiceUnavailable)
		return
	}

	ocSession, err := inst.Client.CreateSession(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Store in our DB too
	sessionID := ocSession.ID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}
	s.queries.CreateSession(r.Context(), sessionID, instanceID, ocSession.Title, "active")

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, ocSession)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	session, err := s.queries.GetSession(r.Context(), id)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	// Try to get from OpenCode
	inst, ok := s.manager.Get(session.InstanceID)
	if ok && inst.Client != nil {
		// Return DB session enriched with instance info
		writeJSON(w, map[string]any{
			"id":          session.ID,
			"instance_id": session.InstanceID,
			"title":       session.Title,
			"status":      session.Status,
			"created_at":  session.CreatedAt,
			"updated_at":  session.UpdatedAt,
		})
		return
	}

	writeJSON(w, session)
}

func (s *Server) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	session, err := s.queries.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	inst, ok := s.manager.Get(session.InstanceID)
	if !ok || inst.Client == nil {
		http.Error(w, "instance not available", http.StatusServiceUnavailable)
		return
	}

	messages, err := inst.Client.GetMessages(r.Context(), sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, messages)
}

type sendMessageRequest struct {
	Content string `json:"content"`
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	session, err := s.queries.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	inst, ok := s.manager.Get(session.InstanceID)
	if !ok || inst.Client == nil {
		http.Error(w, "instance not available", http.StatusServiceUnavailable)
		return
	}

	if err := inst.Client.SendMessage(r.Context(), sessionID, req.Content); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]string{"status": "sent"})
}

func (s *Server) handleSessionEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	s.broker.ServeHTTP(w, r, sessionID)
}
