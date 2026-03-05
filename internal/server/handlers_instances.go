package server

import (
	"encoding/json"
	"net/http"

	"github.com/nbitslabs/flock/internal/db/sqlc"
)

type createInstanceRequest struct {
	WorkingDirectory string `json:"working_directory"`
}

type instanceResponse struct {
	ID               string `json:"id"`
	Pid              int    `json:"pid"`
	Port             int    `json:"port"`
	WorkingDirectory string `json:"working_directory"`
	Status           string `json:"status"`
}

func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := s.queries.ListInstances(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Merge live state from manager
	resp := make([]instanceResponse, len(instances))
	for i, inst := range instances {
		resp[i] = dbInstanceToResponse(inst)
		if live, ok := s.manager.Get(inst.ID); ok {
			resp[i].Status = live.Status
			resp[i].Port = live.Port
			resp[i].Pid = live.Pid
		}
	}
	writeJSON(w, resp)
}

func (s *Server) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, err := s.queries.GetInstance(r.Context(), id)
	if err != nil {
		http.Error(w, "instance not found", http.StatusNotFound)
		return
	}
	resp := dbInstanceToResponse(inst)
	if live, ok := s.manager.Get(id); ok {
		resp.Status = live.Status
		resp.Port = live.Port
		resp.Pid = live.Pid
	}
	writeJSON(w, resp)
}

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var req createInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.WorkingDirectory == "" {
		http.Error(w, "working_directory is required", http.StatusBadRequest)
		return
	}

	inst, err := s.manager.Spawn(r.Context(), req.WorkingDirectory)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, instanceResponse{
		ID:               inst.ID,
		Pid:              inst.Pid,
		Port:             inst.Port,
		WorkingDirectory: inst.WorkingDirectory,
		Status:           inst.Status,
	})
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.manager.Stop(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func dbInstanceToResponse(inst sqlc.Instance) instanceResponse {
	return instanceResponse{
		ID:               inst.ID,
		Pid:              int(inst.Pid),
		Port:             int(inst.Port),
		WorkingDirectory: inst.WorkingDirectory,
		Status:           inst.Status,
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
