package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nbitslabs/flock/internal/db/sqlc"
	"github.com/nbitslabs/flock/internal/memory"
)

const (
	// CleanupGracePeriod is the time to wait after a PR is merged before
	// cleaning up the worktree, giving agents time to finish final writes.
	CleanupGracePeriod = 1 * time.Hour

	// AbandonedRetention is how long to keep worktrees for abandoned tasks
	// (failed, stuck with no activity) before automatic cleanup.
	AbandonedRetention = 7 * 24 * time.Hour

	// MaxCleanupRetries is the maximum number of retry attempts for cleanup.
	MaxCleanupRetries = 3
)

// WorktreeCleaner handles automatic worktree cleanup based on task lifecycle.
type WorktreeCleaner struct {
	queries    *sqlc.Queries
	dataDir    string
	org        string
	repo       string
	instanceID string
}

// NewWorktreeCleaner creates a new WorktreeCleaner.
func NewWorktreeCleaner(queries *sqlc.Queries, dataDir, instanceID, org, repo string) *WorktreeCleaner {
	return &WorktreeCleaner{
		queries:    queries,
		dataDir:    dataDir,
		org:        org,
		repo:       repo,
		instanceID: instanceID,
	}
}

// CleanupResult records the outcome of a cleanup attempt.
type CleanupResult struct {
	TaskID     string
	BranchName string
	Action     string // "removed", "skipped_uncommitted", "skipped_grace", "skipped_retention", "failed"
	Error      error
}

// RunCleanup checks all completed and failed tasks and cleans up their worktrees
// according to retention policies.
func (wc *WorktreeCleaner) RunCleanup(ctx context.Context, instanceID string) []CleanupResult {
	var results []CleanupResult

	// Process completed tasks (merged PRs, closed issues)
	completedTasks, err := wc.queries.ListCompletedTasks(ctx, instanceID)
	if err != nil {
		log.Printf("agent: cleanup: failed to list completed tasks: %v", err)
		return results
	}

	for _, task := range completedTasks {
		result := wc.cleanupCompletedTask(task)
		results = append(results, result)
	}

	// Process failed/abandoned tasks
	failedTasks, err := wc.queries.ListFailedTasks(ctx, instanceID)
	if err != nil {
		log.Printf("agent: cleanup: failed to list failed tasks: %v", err)
		return results
	}

	for _, task := range failedTasks {
		result := wc.cleanupAbandonedTask(task)
		results = append(results, result)
	}

	return results
}

// cleanupCompletedTask handles cleanup for a task that was completed (PR merged,
// issue closed). Enforces the grace period before removal.
func (wc *WorktreeCleaner) cleanupCompletedTask(task sqlc.Task) CleanupResult {
	result := CleanupResult{
		TaskID:     task.ID,
		BranchName: task.BranchName,
	}

	// Check grace period
	completedAt, err := time.Parse("2006-01-02 15:04:05", task.UpdatedAt)
	if err != nil {
		// Try alternate format
		completedAt, err = time.Parse(time.RFC3339, task.UpdatedAt)
		if err != nil {
			log.Printf("agent: cleanup: cannot parse updated_at for task %s: %v", truncID(task.ID), err)
			completedAt = time.Now() // treat as just completed
		}
	}

	if time.Since(completedAt) < CleanupGracePeriod {
		result.Action = "skipped_grace"
		log.Printf("agent: cleanup: task %s still in grace period (completed %s ago)",
			truncID(task.ID), time.Since(completedAt).Round(time.Minute))
		return result
	}

	return wc.doCleanup(task)
}

// cleanupAbandonedTask handles cleanup for failed/stuck tasks.
// Retains worktrees for AbandonedRetention before cleanup.
func (wc *WorktreeCleaner) cleanupAbandonedTask(task sqlc.Task) CleanupResult {
	result := CleanupResult{
		TaskID:     task.ID,
		BranchName: task.BranchName,
	}

	// Check retention period
	lastActivity, err := time.Parse("2006-01-02 15:04:05", task.LastActivityAt)
	if err != nil {
		lastActivity, err = time.Parse(time.RFC3339, task.LastActivityAt)
		if err != nil {
			log.Printf("agent: cleanup: cannot parse last_activity_at for task %s: %v", truncID(task.ID), err)
			result.Action = "skipped_retention"
			return result
		}
	}

	if time.Since(lastActivity) < AbandonedRetention {
		result.Action = "skipped_retention"
		log.Printf("agent: cleanup: abandoned task %s still in retention (inactive %s)",
			truncID(task.ID), time.Since(lastActivity).Round(time.Hour))
		return result
	}

	return wc.doCleanup(task)
}

// doCleanup performs the actual worktree removal with uncommitted-changes check
// and retry logic.
func (wc *WorktreeCleaner) doCleanup(task sqlc.Task) CleanupResult {
	result := CleanupResult{
		TaskID:     task.ID,
		BranchName: task.BranchName,
	}

	wtPath := memory.RepoWorktreePath(wc.dataDir, wc.org, wc.repo, task.BranchName)

	// Check if worktree still exists
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		result.Action = "removed"
		log.Printf("agent: cleanup: worktree for task %s already gone", truncID(task.ID))
		return result
	}

	// Check for uncommitted changes
	if hasUncommitted, err := hasUncommittedChanges(wtPath); err != nil {
		log.Printf("agent: cleanup: failed to check uncommitted changes for %s: %v", truncID(task.ID), err)
	} else if hasUncommitted {
		result.Action = "skipped_uncommitted"
		log.Printf("agent: cleanup: task %s has uncommitted changes, marking for manual review", truncID(task.ID))
		return result
	}

	// Attempt removal with retries and exponential backoff
	var lastErr error
	for attempt := 0; attempt < MaxCleanupRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt)) * time.Second
			log.Printf("agent: cleanup: retrying removal of %s (attempt %d, backoff %s)",
				truncID(task.ID), attempt+1, backoff)
			time.Sleep(backoff)
		}

		if err := RemoveWorktree(wc.dataDir, wc.org, wc.repo, task.BranchName); err != nil {
			lastErr = err
			log.Printf("agent: cleanup: removal attempt %d failed for task %s: %v",
				attempt+1, truncID(task.ID), err)
			continue
		}

		result.Action = "removed"
		log.Printf("agent: cleanup: removed worktree for task %s (branch %s)",
			truncID(task.ID), task.BranchName)
		// Record deletion in metadata
		RecordWorktreeDeletion(context.Background(), wc.queries, wc.instanceID, task.BranchName, task.Status)
		return result
	}

	result.Action = "failed"
	result.Error = fmt.Errorf("cleanup failed after %d retries: %w", MaxCleanupRetries, lastErr)
	log.Printf("agent: cleanup: giving up on task %s after %d retries: %v",
		truncID(task.ID), MaxCleanupRetries, lastErr)
	return result
}

// hasUncommittedChanges checks if the worktree has uncommitted changes.
func hasUncommittedChanges(worktreePath string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = worktreePath
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return strings.TrimSpace(string(output)) != "", nil
}
