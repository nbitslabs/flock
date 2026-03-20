package agent

import (
	"context"
	"log"

	"github.com/google/uuid"
	"github.com/nbitslabs/flock/internal/db/sqlc"
	"github.com/nbitslabs/flock/internal/memory"
)

// RecordWorktreeCreation stores metadata when a worktree is created.
func RecordWorktreeCreation(ctx context.Context, queries *sqlc.Queries, instanceID, dataDir, org, repo, branchName, sessionID string, issueNumber int64) {
	wtPath := memory.RepoWorktreePath(dataDir, org, repo, branchName)

	// Check if already tracked
	if _, err := queries.GetWorktreeByBranch(ctx, sqlc.GetWorktreeByBranchParams{
		InstanceID: instanceID,
		BranchName: branchName,
	}); err == nil {
		// Already tracked — update activity
		return
	}

	id := uuid.New().String()
	if _, err := queries.CreateWorktreeMetadata(ctx, sqlc.CreateWorktreeMetadataParams{
		ID:             id,
		InstanceID:     instanceID,
		BranchName:     branchName,
		WorktreePath:   wtPath,
		IssueNumber:    issueNumber,
		AgentSessionID: sessionID,
	}); err != nil {
		log.Printf("agent: failed to record worktree metadata for %s: %v", branchName, err)
	}
}

// RecordWorktreeActivity updates the last_activity timestamp for a worktree.
func RecordWorktreeActivity(ctx context.Context, queries *sqlc.Queries, instanceID, branchName string) {
	wt, err := queries.GetWorktreeByBranch(ctx, sqlc.GetWorktreeByBranchParams{
		InstanceID: instanceID,
		BranchName: branchName,
	})
	if err != nil {
		return
	}
	queries.UpdateWorktreeActivity(ctx, wt.ID)
}

// RecordWorktreeDeletion records when a worktree is removed and why.
func RecordWorktreeDeletion(ctx context.Context, queries *sqlc.Queries, instanceID, branchName, reason string) {
	wt, err := queries.GetWorktreeByBranch(ctx, sqlc.GetWorktreeByBranchParams{
		InstanceID: instanceID,
		BranchName: branchName,
	})
	if err != nil {
		return
	}
	queries.UpdateWorktreeDeleted(ctx, sqlc.UpdateWorktreeDeletedParams{
		DeletionReason: reason,
		ID:             wt.ID,
	})
}
