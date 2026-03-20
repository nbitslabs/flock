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

// handleListPRSets returns PR sets for a group.
func (s *Server) handleListPRSets(w http.ResponseWriter, r *http.Request) {
	group := r.URL.Query().Get("group")
	if group == "" {
		http.Error(w, "group parameter required", http.StatusBadRequest)
		return
	}

	sets, err := s.queries.ListPRSets(r.Context(), group)
	if err != nil {
		http.Error(w, "failed to list PR sets", http.StatusInternalServerError)
		return
	}

	type prSetResponse struct {
		ID              string `json:"id"`
		GroupName       string `json:"group_name"`
		Status          string `json:"status"`
		DeploymentOrder string `json:"deployment_order,omitempty"`
		MemberCount     int    `json:"member_count"`
		MergedCount     int    `json:"merged_count"`
		CreatedAt       string `json:"created_at"`
	}

	result := make([]prSetResponse, 0, len(sets))
	for _, ps := range sets {
		members, _ := s.queries.ListPRSetMembers(r.Context(), ps.ID)
		mergedCount := 0
		for _, m := range members {
			if m.Status == "merged" {
				mergedCount++
			}
		}
		result = append(result, prSetResponse{
			ID:              ps.ID,
			GroupName:       ps.GroupName,
			Status:          ps.Status,
			DeploymentOrder: ps.DeploymentOrder.String,
			MemberCount:     len(members),
			MergedCount:     mergedCount,
			CreatedAt:       ps.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	writeJSON(w, result)
}

// handleGetPRSet returns details of a specific PR set.
func (s *Server) handleGetPRSet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ps, err := s.queries.GetPRSet(r.Context(), id)
	if err != nil {
		http.Error(w, "PR set not found", http.StatusNotFound)
		return
	}

	members, err := s.queries.ListPRSetMembers(r.Context(), ps.ID)
	if err != nil {
		http.Error(w, "failed to list members", http.StatusInternalServerError)
		return
	}

	type memberResponse struct {
		ID         string `json:"id"`
		InstanceID string `json:"instance_id"`
		Org        string `json:"org"`
		Repo       string `json:"repo"`
		PrUrl      string `json:"pr_url"`
		PrNumber   int64  `json:"pr_number"`
		Status     string `json:"status"`
		MergeOrder int64  `json:"merge_order"`
		MergedAt   string `json:"merged_at,omitempty"`
	}

	memberList := make([]memberResponse, len(members))
	for i, m := range members {
		mr := memberResponse{
			ID:         m.ID,
			InstanceID: m.InstanceID,
			Org:        m.Org,
			Repo:       m.Repo,
			PrUrl:      m.PrUrl,
			PrNumber:   m.PrNumber,
			Status:     m.Status,
			MergeOrder: m.MergeOrder,
		}
		if m.MergedAt.Valid {
			mr.MergedAt = m.MergedAt.Time.Format("2006-01-02T15:04:05Z")
		}
		memberList[i] = mr
	}

	writeJSON(w, map[string]any{
		"id":               ps.ID,
		"group_name":       ps.GroupName,
		"status":           ps.Status,
		"deployment_order": ps.DeploymentOrder.String,
		"members":          memberList,
		"created_at":       ps.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// handleDashboardSync returns cross-repo sync status: cross-repo tasks, PR sets, and dependency info.
func (s *Server) handleDashboardSync(w http.ResponseWriter, r *http.Request) {
	group := r.URL.Query().Get("group")

	// Get all cross-repo tasks
	instances, err := s.queries.ListInstances(r.Context())
	if err != nil {
		http.Error(w, "failed to list instances", http.StatusInternalServerError)
		return
	}

	type crossRepoTaskInfo struct {
		ID              string `json:"id"`
		ParentTaskID    string `json:"parent_task_id"`
		ChildTaskID     string `json:"child_task_id"`
		ChildInstanceID string `json:"child_instance_id"`
		Status          string `json:"status"`
		ChildOrg        string `json:"child_org,omitempty"`
		ChildRepo       string `json:"child_repo,omitempty"`
		CreatedAt       string `json:"created_at"`
	}

	var crossRepoTasks []crossRepoTaskInfo

	// Collect tasks from all instances
	for _, inst := range instances {
		tasks, err := s.queries.ListTasksByInstance(r.Context(), inst.ID)
		if err != nil {
			continue
		}
		for _, t := range tasks {
			children, err := s.queries.ListCrossRepoTasksByParent(r.Context(), t.ID)
			if err != nil {
				continue
			}
			for _, child := range children {
				info := crossRepoTaskInfo{
					ID:              child.ID,
					ParentTaskID:    child.ParentTaskID,
					ChildTaskID:     child.ChildTaskID,
					ChildInstanceID: child.ChildInstanceID,
					Status:          child.Status,
					CreatedAt:       child.CreatedAt.Format("2006-01-02T15:04:05Z"),
				}
				// Resolve child instance org/repo
				childInst, err := s.queries.GetInstance(r.Context(), child.ChildInstanceID)
				if err == nil {
					info.ChildOrg = childInst.Org
					info.ChildRepo = childInst.Repo
				}
				crossRepoTasks = append(crossRepoTasks, info)
			}
		}
	}

	if crossRepoTasks == nil {
		crossRepoTasks = []crossRepoTaskInfo{}
	}

	// Get PR sets if group provided
	type prSetSummary struct {
		ID          string `json:"id"`
		GroupName   string `json:"group_name"`
		Status      string `json:"status"`
		MemberCount int    `json:"member_count"`
		MergedCount int    `json:"merged_count"`
	}
	var prSets []prSetSummary

	if group != "" {
		sets, err := s.queries.ListPRSets(r.Context(), group)
		if err == nil {
			for _, ps := range sets {
				members, _ := s.queries.ListPRSetMembers(r.Context(), ps.ID)
				merged := 0
				for _, m := range members {
					if m.Status == "merged" {
						merged++
					}
				}
				prSets = append(prSets, prSetSummary{
					ID:          ps.ID,
					GroupName:   ps.GroupName,
					Status:      ps.Status,
					MemberCount: len(members),
					MergedCount: merged,
				})
			}
		}
	}

	if prSets == nil {
		prSets = []prSetSummary{}
	}

	// Get dependency graph if group provided
	var graphData map[string]any
	if group != "" {
		manifests, err := s.queries.ListManifestsByGroup(r.Context(), group)
		if err == nil {
			manifestMap := make(map[string]*crossrepo.Manifest)
			for _, m := range manifests {
				parsed, err := crossrepo.ParseManifest(m.ManifestJson)
				if err != nil {
					continue
				}
				manifestMap[m.Org+"/"+m.Repo] = parsed
			}
			graph, err := crossrepo.BuildDependencyGraph(manifestMap)
			if err == nil {
				type edgeInfo struct {
					From string `json:"from"`
					To   string `json:"to"`
					Type string `json:"type"`
				}
				edges := make([]edgeInfo, len(graph.Edges))
				for i, e := range graph.Edges {
					edges[i] = edgeInfo{From: e.From, To: e.To, Type: e.Type}
				}
				graphData = map[string]any{
					"nodes":            graph.Nodes,
					"edges":            edges,
					"deployment_order": graph.DeploymentOrder(),
				}
			}
		}
	}

	writeJSON(w, map[string]any{
		"cross_repo_tasks": crossRepoTasks,
		"pr_sets":          prSets,
		"dependency_graph": graphData,
	})
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
