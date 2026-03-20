package server

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/nbitslabs/flock/internal/crossrepo"
	"github.com/nbitslabs/flock/internal/db/sqlc"
)

// handleGetManifest returns the manifest for a repository.
func (s *Server) handleGetManifest(w http.ResponseWriter, r *http.Request) {
	instanceID := r.PathValue("id")
	inst, err := s.queries.GetInstance(r.Context(), instanceID)
	if err != nil {
		http.Error(w, "instance not found", http.StatusNotFound)
		return
	}

	manifest, err := s.queries.GetRepoManifest(r.Context(), sqlc.GetRepoManifestParams{
		InstanceID: instanceID,
		Org:        inst.Org,
		Repo:       inst.Repo,
	})
	if err != nil {
		http.Error(w, "manifest not found", http.StatusNotFound)
		return
	}

	writeJSON(w, map[string]any{
		"id":               manifest.ID,
		"org":              manifest.Org,
		"repo":             manifest.Repo,
		"group_name":       manifest.GroupName,
		"manifest":         json.RawMessage(manifest.ManifestJson),
		"valid":            manifest.Valid,
		"validation_error": manifest.ValidationError.String,
	})
}

// handlePutManifest creates or updates a repository manifest.
func (s *Server) handlePutManifest(w http.ResponseWriter, r *http.Request) {
	instanceID := r.PathValue("id")
	inst, err := s.queries.GetInstance(r.Context(), instanceID)
	if err != nil {
		http.Error(w, "instance not found", http.StatusNotFound)
		return
	}

	var req struct {
		Manifest json.RawMessage `json:"manifest"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Parse and validate
	m, err := crossrepo.ParseManifest(string(req.Manifest))
	if err != nil {
		http.Error(w, "invalid manifest: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := crossrepo.ValidateManifest(m); err != nil {
		http.Error(w, "invalid manifest: "+err.Error(), http.StatusBadRequest)
		return
	}

	manifest, err := s.queries.UpsertRepoManifest(r.Context(), sqlc.UpsertRepoManifestParams{
		ID:           uuid.New().String(),
		InstanceID:   instanceID,
		Org:          inst.Org,
		Repo:         inst.Repo,
		GroupName:    m.Group,
		ManifestJson: string(req.Manifest),
	})
	if err != nil {
		http.Error(w, "failed to save manifest: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"id":         manifest.ID,
		"org":        manifest.Org,
		"repo":       manifest.Repo,
		"group_name": manifest.GroupName,
	})
}

// handleListManifests returns all manifests, optionally filtered by group.
func (s *Server) handleListManifests(w http.ResponseWriter, r *http.Request) {
	group := r.URL.Query().Get("group")

	var manifests []sqlc.RepoManifest
	var err error
	if group != "" {
		manifests, err = s.queries.ListManifestsByGroup(r.Context(), group)
	} else {
		manifests, err = s.queries.ListAllManifests(r.Context())
	}
	if err != nil {
		http.Error(w, "failed to list manifests: "+err.Error(), http.StatusInternalServerError)
		return
	}

	type manifestResponse struct {
		ID              string          `json:"id"`
		Org             string          `json:"org"`
		Repo            string          `json:"repo"`
		GroupName       string          `json:"group_name"`
		Manifest        json.RawMessage `json:"manifest"`
		Valid           bool            `json:"valid"`
		ValidationError string          `json:"validation_error,omitempty"`
	}

	result := make([]manifestResponse, len(manifests))
	for i, m := range manifests {
		result[i] = manifestResponse{
			ID:              m.ID,
			Org:             m.Org,
			Repo:            m.Repo,
			GroupName:       m.GroupName,
			Manifest:        json.RawMessage(m.ManifestJson),
			Valid:           m.Valid,
			ValidationError: m.ValidationError.String,
		}
	}

	writeJSON(w, result)
}

// handleGetDependencyGraph returns the dependency graph for a group.
func (s *Server) handleGetDependencyGraph(w http.ResponseWriter, r *http.Request) {
	group := r.URL.Query().Get("group")
	if group == "" {
		http.Error(w, "group parameter required", http.StatusBadRequest)
		return
	}

	manifests, err := s.queries.ListManifestsByGroup(r.Context(), group)
	if err != nil {
		http.Error(w, "failed to list manifests", http.StatusInternalServerError)
		return
	}

	manifestMap := make(map[string]*crossrepo.Manifest)
	for _, m := range manifests {
		parsed, err := crossrepo.ParseManifest(m.ManifestJson)
		if err != nil {
			continue
		}
		key := m.Org + "/" + m.Repo
		manifestMap[key] = parsed
	}

	graph, err := crossrepo.BuildDependencyGraph(manifestMap)
	if err != nil {
		writeJSON(w, map[string]any{
			"error":            err.Error(),
			"deployment_order": []string{},
		})
		return
	}

	type edgeResponse struct {
		From string `json:"from"`
		To   string `json:"to"`
		Type string `json:"type"`
	}

	edges := make([]edgeResponse, len(graph.Edges))
	for i, e := range graph.Edges {
		edges[i] = edgeResponse{From: e.From, To: e.To, Type: e.Type}
	}

	writeJSON(w, map[string]any{
		"nodes":            graph.Nodes,
		"edges":            edges,
		"deployment_order": graph.DeploymentOrder(),
	})
}
