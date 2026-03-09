package server

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/nbitslabs/flock/internal/db/sqlc"
	"github.com/nbitslabs/flock/internal/opencode"
)

const flockAgentInstanceID = "flock-agent"

func (s *Server) getClientForSession(session *sqlc.Session) *opencode.Client {
	if session.InstanceID == flockAgentInstanceID {
		return s.flockAgentClient
	}
	inst, ok := s.manager.Get(session.InstanceID)
	if !ok {
		return nil
	}
	return inst.Client
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
		sessions, err := inst.Client.ListSessions(r.Context(), inst.WorkingDirectory)
		if err == nil {
			// Build set of session IDs in this directory
			ownIDs := make(map[string]struct{}, len(sessions))
			for _, sess := range sessions {
				ownIDs[sess.ID] = struct{}{}
			}

			// Filter out sessions whose parent belongs to a different instance
			// (e.g. sub-agent sessions created in flock's dir by an MCP session)
			var result []opencode.Session
			for _, sess := range sessions {
				if sess.ParentID != "" {
					if _, parentIsOurs := ownIDs[sess.ParentID]; !parentIsOurs {
						continue
					}
				}
				result = append(result, sess)
			}

			// Include children of our sessions that live in other directories
			// (e.g. @flock-commit-writer sessions spawned by our sub-agent)
			// Snapshot length to avoid iterating over appended children.
			n := len(result)
			for i := 0; i < n; i++ {
				children, err := inst.Client.ListSessionChildren(r.Context(), result[i].ID)
				if err != nil {
					continue
				}
				for _, child := range children {
					if child.Directory != inst.WorkingDirectory {
						result = append(result, child)
					}
				}
			}

			for _, sess := range result {
				s.queries.UpsertSession(r.Context(), sqlc.UpsertSessionParams{
					ID:         sess.ID,
					InstanceID: instanceID,
					ParentID:   sess.ParentID,
					Title:      sess.Title,
					Status:     "active",
				})
			}
			writeJSON(w, result)
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

	ocSession, err := inst.Client.CreateSession(r.Context(), inst.WorkingDirectory)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Store in our DB too
	sessionID := ocSession.ID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}
	s.queries.CreateSession(r.Context(), sqlc.CreateSessionParams{
		ID:         sessionID,
		InstanceID: instanceID,
		Title:      ocSession.Title,
		Status:     "active",
	})

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

	client := s.getClientForSession(&session)
	if client != nil {
		writeJSON(w, map[string]any{
			"id":          session.ID,
			"instance_id": session.InstanceID,
			"title":       session.Title,
			"status":      session.Status,
			"model":       session.Model.String,
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

	client := s.getClientForSession(&session)
	if client == nil {
		http.Error(w, "instance not available", http.StatusServiceUnavailable)
		return
	}

	messages, err := client.GetMessages(r.Context(), sessionID)
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

	client := s.getClientForSession(&session)
	if client == nil {
		http.Error(w, "instance not available", http.StatusServiceUnavailable)
		return
	}

	if err := client.SendMessage(r.Context(), sessionID, req.Content, session.Model.String); err != nil {
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

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	session, err := s.queries.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	client := s.getClientForSession(&session)
	if client != nil {
		if err := client.DeleteSession(r.Context(), sessionID); err != nil {
			log.Printf("failed to delete session from opencode: %v", err)
		}
	}

	if err := s.queries.DeleteSession(r.Context(), sessionID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetModels(w http.ResponseWriter, r *http.Request) {
	instanceID := r.PathValue("id")
	inst, ok := s.manager.Get(instanceID)
	if !ok || inst.Client == nil {
		http.Error(w, "instance not available", http.StatusServiceUnavailable)
		return
	}

	providers, err := inst.Client.GetProviders(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, providers)
}

type setModelRequest struct {
	Model string `json:"model"`
}

func (s *Server) handleSetSessionModel(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if _, err := s.queries.GetSession(r.Context(), sessionID); err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	var req setModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	model := sql.NullString{String: req.Model, Valid: req.Model != ""}
	if err := s.queries.UpdateSessionModel(r.Context(), sqlc.UpdateSessionModelParams{
		Model: model,
		ID:    sessionID,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"model": req.Model})
}
