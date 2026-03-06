package server

import (
	"encoding/json"
	"io"
	"net/http"

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
