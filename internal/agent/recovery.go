package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/nbitslabs/flock/internal/db/sqlc"
	"github.com/nbitslabs/flock/internal/memory"
)

// RecoveryAction records what the recovery system did for a worktree.
type RecoveryAction struct {
	WorktreeID string
	BranchName string
	Action     string // "repaired", "recreated", "failed"
	Details    string
}

// WorktreeRecovery handles detection and repair of corrupted worktrees.
type WorktreeRecovery struct {
	queries        *sqlc.Queries
	client         interface{ /* not used directly */ }
	instanceID     string
	dataDir        string
	org            string
	repo           string
	sourceRepoPath string
}

// NewWorktreeRecovery creates a new WorktreeRecovery.
func NewWorktreeRecovery(queries *sqlc.Queries, instanceID, dataDir, org, repo, sourceRepoPath string) *WorktreeRecovery {
	return &WorktreeRecovery{
		queries:        queries,
		instanceID:     instanceID,
		dataDir:        dataDir,
		org:            org,
		repo:           repo,
		sourceRepoPath: sourceRepoPath,
	}
}

// RecoverCorruptedWorktrees processes health check results and attempts to
// repair or recreate any corrupted or missing worktrees.
func (wr *WorktreeRecovery) RecoverCorruptedWorktrees(ctx context.Context, healthResults []HealthCheckResult) []RecoveryAction {
	var actions []RecoveryAction

	for _, result := range healthResults {
		if result.Status != "corrupted" && result.Status != "missing" {
			continue
		}

		wt, err := wr.queries.GetWorktreeByID(ctx, result.WorktreeID)
		if err != nil {
			log.Printf("agent: recovery: failed to get worktree %s: %v", truncID(result.WorktreeID), err)
			continue
		}

		action := wr.recoverWorktree(ctx, wt, result)
		actions = append(actions, action)
	}

	return actions
}

// recoverWorktree attempts to repair a single corrupted worktree.
func (wr *WorktreeRecovery) recoverWorktree(ctx context.Context, wt sqlc.WorktreeMetadatum, result HealthCheckResult) RecoveryAction {
	action := RecoveryAction{
		WorktreeID: wt.ID,
		BranchName: wt.BranchName,
	}

	wtPath := wt.WorktreePath

	// Step 1: Try git worktree repair
	if result.Status == "corrupted" {
		if repaired := wr.attemptRepair(wtPath); repaired {
			// Validate repair
			if ok, _ := runGitFsck(wtPath); ok {
				action.Action = "repaired"
				action.Details = "git worktree repair succeeded, fsck passed"
				log.Printf("agent: recovery: repaired worktree %s at %s", truncID(wt.ID), wtPath)

				// Update metadata
				wr.queries.UpdateWorktreeStatus(ctx, sqlc.UpdateWorktreeStatusParams{
					Status: "active",
					ID:     wt.ID,
				})
				return action
			}
			log.Printf("agent: recovery: repair did not fix corruption for %s", truncID(wt.ID))
		}
	}

	// Step 2: Remove and recreate the worktree
	recreated := wr.recreateWorktree(ctx, wt)
	if recreated {
		action.Action = "recreated"
		action.Details = fmt.Sprintf("removed corrupted worktree and created fresh one at %s", wtPath)
		log.Printf("agent: recovery: recreated worktree %s (branch %s)", truncID(wt.ID), wt.BranchName)

		// Update metadata to active
		wr.queries.UpdateWorktreeStatus(ctx, sqlc.UpdateWorktreeStatusParams{
			Status: "active",
			ID:     wt.ID,
		})

		// Write a recovery marker so the restarted agent knows recovery occurred
		wr.writeRecoveryMarker(wtPath, wt.BranchName, result.ErrorMessage)
	} else {
		action.Action = "failed"
		action.Details = "repair and recreation both failed"
		log.Printf("agent: recovery: failed to recover worktree %s", truncID(wt.ID))
	}

	return action
}

// attemptRepair runs git worktree repair on the source repo.
func (wr *WorktreeRecovery) attemptRepair(wtPath string) bool {
	// git worktree repair must be run from the main repo
	cmd := exec.Command("git", "worktree", "repair")
	cmd.Dir = wr.sourceRepoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("agent: recovery: git worktree repair failed: %v\n%s", err, string(output))
		return false
	}
	log.Printf("agent: recovery: git worktree repair output: %s", strings.TrimSpace(string(output)))
	return true
}

// recreateWorktree removes a corrupted worktree and creates a fresh one.
func (wr *WorktreeRecovery) recreateWorktree(ctx context.Context, wt sqlc.WorktreeMetadatum) bool {
	wtPath := wt.WorktreePath

	// Force remove the corrupted worktree directory
	if _, err := os.Stat(wtPath); err == nil {
		// Try git worktree remove first
		cmd := exec.Command("git", "worktree", "remove", "--force", wtPath)
		cmd.Dir = wr.sourceRepoPath
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("agent: recovery: git worktree remove failed, removing directory: %v\n%s", err, string(output))
			// Fall back to direct removal
			if err := os.RemoveAll(wtPath); err != nil {
				log.Printf("agent: recovery: failed to remove directory %s: %v", wtPath, err)
				return false
			}
		}

		// Prune stale worktree entries
		pruneCmd := exec.Command("git", "worktree", "prune")
		pruneCmd.Dir = wr.sourceRepoPath
		pruneCmd.CombinedOutput()
	}

	// Recreate the worktree
	_, err := EnsureWorktree(wr.dataDir, wr.org, wr.repo, wt.BranchName, wr.sourceRepoPath)
	if err != nil {
		log.Printf("agent: recovery: failed to recreate worktree for %s: %v", wt.BranchName, err)
		return false
	}

	return true
}

// writeRecoveryMarker writes a file in the recovered worktree indicating
// that recovery occurred, so the restarted agent knows what happened.
func (wr *WorktreeRecovery) writeRecoveryMarker(wtPath, branchName, originalError string) {
	markerPath := memory.RepoProgressPath(wr.dataDir, wr.org, wr.repo)
	os.MkdirAll(markerPath, 0o755)

	content := fmt.Sprintf(`# Worktree Recovery

The worktree for branch %s was automatically recovered.

## Original Error
%s

## Recovery Action
The corrupted worktree was removed and recreated. Any uncommitted
changes from the previous session were lost. The branch still contains
all previously committed work.

## Next Steps
- The task will be restarted by the orchestrator.
- Review the branch history to understand where the previous agent left off.
- Continue implementation from the last committed state.
`, "`"+branchName+"`", originalError)

	os.WriteFile(
		fmt.Sprintf("%s/recovery_%s.md", markerPath, strings.ReplaceAll(branchName, "/", "_")),
		[]byte(content),
		0o644,
	)
}

// NeedsRestart returns the task IDs that need orchestrator restart due to
// worktree recovery.
func NeedsRestart(actions []RecoveryAction) []string {
	var taskIDs []string
	for _, a := range actions {
		if a.Action == "recreated" {
			taskIDs = append(taskIDs, a.WorktreeID)
		}
	}
	return taskIDs
}
