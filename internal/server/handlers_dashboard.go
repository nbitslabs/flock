package server

import (
	"net/http"
)

// handleDashboardTasks returns all tasks across all instances for the dashboard.
func (s *Server) handleDashboardTasks(w http.ResponseWriter, r *http.Request) {
	instances, err := s.queries.ListInstances(r.Context())
	if err != nil {
		http.Error(w, "failed to list instances", http.StatusInternalServerError)
		return
	}

	type dashboardTask struct {
		ID             string `json:"id"`
		InstanceID     string `json:"instance_id"`
		Org            string `json:"org"`
		Repo           string `json:"repo"`
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

	var allTasks []dashboardTask

	for _, inst := range instances {
		tasks, err := s.queries.ListTasksByInstance(r.Context(), inst.ID)
		if err != nil {
			continue
		}
		for _, t := range tasks {
			allTasks = append(allTasks, dashboardTask{
				ID:             t.ID,
				InstanceID:     inst.ID,
				Org:            inst.Org,
				Repo:           inst.Repo,
				IssueNumber:    t.IssueNumber,
				IssueURL:       t.IssueUrl,
				Title:          t.Title,
				Status:         t.Status,
				SessionID:      t.SessionID,
				BranchName:     t.BranchName,
				PRURL:          t.PrUrl,
				LastActivityAt: t.LastActivityAt,
				CreatedAt:      t.CreatedAt,
			})
		}
	}

	if allTasks == nil {
		allTasks = []dashboardTask{}
	}

	writeJSON(w, allTasks)
}

// handleDashboardWorktrees returns all worktrees across all instances.
func (s *Server) handleDashboardWorktrees(w http.ResponseWriter, r *http.Request) {
	instances, err := s.queries.ListInstances(r.Context())
	if err != nil {
		http.Error(w, "failed to list instances", http.StatusInternalServerError)
		return
	}

	type worktreeInfo struct {
		ID                    string `json:"id"`
		InstanceID            string `json:"instance_id"`
		BranchName            string `json:"branch_name"`
		WorktreePath          string `json:"worktree_path"`
		IssueNumber           int64  `json:"issue_number"`
		Status                string `json:"status"`
		DiskUsageBytes        int64  `json:"disk_usage_bytes"`
		HasUncommittedChanges bool   `json:"has_uncommitted_changes"`
		LastActivityAt        string `json:"last_activity_at"`
		CreatedAt             string `json:"created_at"`
	}

	var allWorktrees []worktreeInfo

	for _, inst := range instances {
		worktrees, err := s.queries.ListAllWorktrees(r.Context(), inst.ID)
		if err != nil {
			continue
		}
		for _, wt := range worktrees {
			allWorktrees = append(allWorktrees, worktreeInfo{
				ID:                    wt.ID,
				InstanceID:            inst.ID,
				BranchName:            wt.BranchName,
				WorktreePath:          wt.WorktreePath,
				IssueNumber:           wt.IssueNumber,
				Status:                wt.Status,
				DiskUsageBytes:        wt.DiskUsageBytes,
				HasUncommittedChanges: wt.HasUncommittedChanges != 0,
				LastActivityAt:        wt.LastActivityAt,
				CreatedAt:             wt.CreatedAt,
			})
		}
	}

	if allWorktrees == nil {
		allWorktrees = []worktreeInfo{}
	}

	writeJSON(w, allWorktrees)
}

// handleDashboardMemoryStats returns memory statistics across all repos.
func (s *Server) handleDashboardMemoryStats(w http.ResponseWriter, r *http.Request) {
	instances, err := s.queries.ListInstances(r.Context())
	if err != nil {
		http.Error(w, "failed to list instances", http.StatusInternalServerError)
		return
	}

	type repoStat struct {
		Org            string            `json:"org"`
		Repo           string            `json:"repo"`
		FileCount      int               `json:"file_count"`
		TotalSizeBytes int64             `json:"total_size_bytes"`
		ByCategory     map[string]int    `json:"by_category"`
	}

	var stats []repoStat

	for _, inst := range instances {
		if inst.Org == "" || inst.Repo == "" {
			continue
		}

		results := s.searchMemoryFiles(inst.Org, inst.Repo, memoryQueryRequest{Limit: 1000})

		categoryCounts := make(map[string]int)
		var totalSize int64
		for _, result := range results {
			categoryCounts[result.Category]++
			totalSize += int64(len(result.Snippet))
		}

		stats = append(stats, repoStat{
			Org:            inst.Org,
			Repo:           inst.Repo,
			FileCount:      len(results),
			TotalSizeBytes: totalSize,
			ByCategory:     categoryCounts,
		})
	}

	if stats == nil {
		stats = []repoStat{}
	}

	writeJSON(w, stats)
}
