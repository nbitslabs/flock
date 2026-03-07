package server

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nbitslabs/flock/internal/db/sqlc"
)

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

type createInstanceRequest struct {
	GitHubURL string `json:"github_url"`
}

type instanceResponse struct {
	ID               string `json:"id"`
	WorkingDirectory string `json:"working_directory"`
	Org              string `json:"org,omitempty"`
	Repo             string `json:"repo,omitempty"`
	Status           string `json:"status"`
	LastHeartbeatAt  string `json:"last_heartbeat_at,omitempty"`
}

func parseGitHubURL(url string) (org, repo string, err error) {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, ".git")

	var path string

	if strings.HasPrefix(url, "https://github.com/") {
		path = strings.TrimPrefix(url, "https://github.com/")
	} else if strings.HasPrefix(url, "git@github.com:") {
		path = strings.TrimPrefix(url, "git@github.com:")
	} else if strings.HasPrefix(url, "github.com/") {
		path = strings.TrimPrefix(url, "github.com/")
	} else {
		return "", "", nil
	}

	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", "", nil
	}

	return parts[0], parts[1], nil
}

func cloneOrGetRepo(basePath, org, repo string) (string, error) {
	repoPath := filepath.Join(basePath, "github.com", org, repo)

	if _, err := os.Stat(repoPath); err == nil {
		return repoPath, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	orgPath := filepath.Join(basePath, "github.com", org)
	if err := os.MkdirAll(orgPath, 0755); err != nil {
		return "", err
	}

	gitURL := "git@github.com:" + org + "/" + repo + ".git"
	cmd := exec.Command("git", "clone", "--depth", "1", gitURL, repoPath)
	cmd.Dir = orgPath
	if err := cmd.Run(); err != nil {
		return "", err
	}

	return repoPath, nil
}

func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := s.queries.ListInstances(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := make([]instanceResponse, len(instances))
	for i, inst := range instances {
		resp[i] = dbInstanceToResponse(inst)
		if live, ok := s.manager.Get(inst.ID); ok {
			resp[i].Status = live.Status
		}
		if hb, err := s.queries.GetLastHeartbeatByInstance(r.Context(), inst.ID); err == nil && hb != "" {
			resp[i].LastHeartbeatAt = hb
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
	}
	if hb, err := s.queries.GetLastHeartbeatByInstance(r.Context(), id); err == nil && hb != "" {
		resp.LastHeartbeatAt = hb
	}
	writeJSON(w, resp)
}

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var req createInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.GitHubURL == "" {
		http.Error(w, "github_url is required", http.StatusBadRequest)
		return
	}

	org, repo, err := parseGitHubURL(req.GitHubURL)
	if err != nil {
		http.Error(w, "invalid GitHub URL: "+err.Error(), http.StatusBadRequest)
		return
	}
	if org == "" || repo == "" {
		http.Error(w, "invalid GitHub URL format. Expected: https://github.com/org/repo", http.StatusBadRequest)
		return
	}

	workingDir, err := cloneOrGetRepo(s.basePath, org, repo)
	if err != nil {
		http.Error(w, "failed to clone repository: "+err.Error(), http.StatusInternalServerError)
		return
	}

	inst, err := s.manager.Register(r.Context(), workingDir, org, repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, instanceResponse{
		ID:               inst.ID,
		WorkingDirectory: inst.WorkingDirectory,
		Org:              inst.Org,
		Repo:             inst.Repo,
		Status:           inst.Status,
	})
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.manager.Stop(r.Context(), id); err != nil {
		// Instance not in memory — clean up DB directly
		s.queries.DeleteSessionsByInstance(r.Context(), id)
		if err := s.queries.DeleteInstance(r.Context(), id); err != nil {
			http.Error(w, "instance not found", http.StatusNotFound)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func dbInstanceToResponse(inst sqlc.Instance) instanceResponse {
	return instanceResponse{
		ID:               inst.ID,
		WorkingDirectory: inst.WorkingDirectory,
		Org:              inst.Org,
		Repo:             inst.Repo,
		Status:           inst.Status,
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
