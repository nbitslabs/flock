package crossrepo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/nbitslabs/flock/internal/db/sqlc"
)

// CrossRepoTaskManager handles creating and tracking cross-repository tasks.
type CrossRepoTaskManager struct {
	queries *sqlc.Queries
}

// NewCrossRepoTaskManager creates a new cross-repo task manager.
func NewCrossRepoTaskManager(queries *sqlc.Queries) *CrossRepoTaskManager {
	return &CrossRepoTaskManager{queries: queries}
}

// CreateCrossRepoTasks creates linked child tasks for affected repositories.
func (m *CrossRepoTaskManager) CreateCrossRepoTasks(ctx context.Context, parentTaskID string, affectedRepos []AffectedRepo) ([]sqlc.CrossRepoTask, error) {
	var created []sqlc.CrossRepoTask

	for _, repo := range affectedRepos {
		task, err := m.queries.CreateCrossRepoTask(ctx, sqlc.CreateCrossRepoTaskParams{
			ID:              uuid.New().String(),
			ParentTaskID:    parentTaskID,
			ChildTaskID:     repo.TaskID,
			ChildInstanceID: repo.InstanceID,
		})
		if err != nil {
			return nil, fmt.Errorf("create cross-repo task for %s/%s: %w", repo.Org, repo.Repo, err)
		}
		created = append(created, task)
	}

	return created, nil
}

// AffectedRepo represents a repository affected by a cross-repo change.
type AffectedRepo struct {
	Org        string
	Repo       string
	InstanceID string
	TaskID     string
	Context    string // Description of how this repo is affected
}

// GetRelatedTasks returns all cross-repo tasks related to a parent task.
func (m *CrossRepoTaskManager) GetRelatedTasks(ctx context.Context, parentTaskID string) ([]sqlc.CrossRepoTask, error) {
	return m.queries.ListCrossRepoTasksByParent(ctx, parentTaskID)
}

// MarkChildComplete marks a child task as completed and checks if all siblings are done.
func (m *CrossRepoTaskManager) MarkChildComplete(ctx context.Context, childTaskID string) (allComplete bool, err error) {
	// Find the cross-repo link for this child
	links, err := m.queries.ListCrossRepoTasksByChild(ctx, childTaskID)
	if err != nil || len(links) == 0 {
		return false, err
	}

	// Update status
	for _, link := range links {
		if err := m.queries.UpdateCrossRepoTaskStatus(ctx, sqlc.UpdateCrossRepoTaskStatusParams{
			Status: "completed",
			ID:     link.ID,
		}); err != nil {
			return false, fmt.Errorf("update cross-repo task status: %w", err)
		}

		// Check if all siblings under same parent are completed
		siblings, err := m.queries.ListCrossRepoTasksByParent(ctx, link.ParentTaskID)
		if err != nil {
			return false, err
		}

		allDone := true
		for _, s := range siblings {
			if s.Status != "completed" {
				allDone = false
				break
			}
		}

		if allDone {
			return true, nil
		}
	}

	return false, nil
}

// DetectAffectedRepos determines which repositories are affected by changes
// in the given repository, using the dependency graph.
func DetectAffectedRepos(graph *DependencyGraph, changedRepo string) []string {
	return graph.AffectedRepositories(changedRepo)
}

// BuildCrossRepoPromptContext creates a prompt context string for agents
// working on cross-repo tasks.
func BuildCrossRepoPromptContext(parentTask string, siblings []AffectedRepo) string {
	if len(siblings) == 0 {
		return ""
	}

	ctx := fmt.Sprintf("## Cross-Repository Task Context\n\n")
	ctx += fmt.Sprintf("This task is part of a coordinated cross-repo change (parent: %s).\n\n", parentTask)
	ctx += "### Related repositories:\n"
	for _, s := range siblings {
		ctx += fmt.Sprintf("- **%s/%s**: %s\n", s.Org, s.Repo, s.Context)
	}
	ctx += "\nEnsure your changes are compatible with the related repositories.\n"
	return ctx
}
