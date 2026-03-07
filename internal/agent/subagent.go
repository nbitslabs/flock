package agent

import (
	"context"
	"fmt"
	"log"

	"github.com/nbitslabs/flock/internal/db/sqlc"
	"github.com/nbitslabs/flock/internal/memory"
	"github.com/nbitslabs/flock/internal/opencode"
)

// CreateSubAgentSession creates a new OpenCode session and sends an issue-resolution prompt.
// The session and DB records are created synchronously, but the actual message send
// runs in a goroutine so it does not block the heartbeat loop.
func CreateSubAgentSession(
	ctx context.Context,
	client *opencode.Client,
	queries *sqlc.Queries,
	instanceID string,
	workingDir string,
	task *sqlc.Task,
) error {
	session, err := client.CreateSession(ctx, workingDir)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	// Store session in DB
	if _, err := queries.CreateSession(ctx, sqlc.CreateSessionParams{
		ID:         session.ID,
		InstanceID: instanceID,
		Title:      fmt.Sprintf("Issue #%d: %s", task.IssueNumber, task.Title),
		Status:     "active",
	}); err != nil {
		// Session already exists is OK (upsert)
		queries.UpsertSession(ctx, sqlc.UpsertSessionParams{
			ID:         session.ID,
			InstanceID: instanceID,
			Title:      fmt.Sprintf("Issue #%d: %s", task.IssueNumber, task.Title),
			Status:     "active",
		})
	}

	// Link session to task
	queries.UpdateTaskSession(ctx, sqlc.UpdateTaskSessionParams{
		SessionID: session.ID,
		ID:        task.ID,
	})
	queries.UpdateTaskStatus(ctx, sqlc.UpdateTaskStatusParams{
		Status: "active",
		ID:     task.ID,
	})

	log.Printf("agent: created sub-agent session %s for issue #%d", session.ID[:8], task.IssueNumber)

	prompt := composeSubAgentPrompt(workingDir, task)

	// Send message in a goroutine so it doesn't block the heartbeat loop.
	// OpenCode's message endpoint blocks until the AI finishes processing.
	go func() {
		if err := client.SendMessage(ctx, session.ID, prompt); err != nil {
			log.Printf("agent: sub-agent message failed for issue #%d: %v", task.IssueNumber, err)
			queries.UpdateTaskStatus(context.Background(), sqlc.UpdateTaskStatusParams{
				Status: "failed",
				ID:     task.ID,
			})
		}
	}()

	return nil
}

func composeSubAgentPrompt(workingDir string, task *sqlc.Task) string {
	worktreeDir := fmt.Sprintf("%s/worktrees/%s", memory.ResolveStateDir(workingDir), task.BranchName)

	return fmt.Sprintf(`You are an autonomous coding agent resolving a GitHub issue. Work independently to completion.

## Setup — Git Worktree
Each sub-agent works in its own git worktree to avoid interfering with other agents. Run these commands first:

`+"```bash"+`
cd %s
git worktree add -b %s %s
cd %s
`+"```"+`

All subsequent work MUST happen inside the worktree directory: `+"`%s`"+`

## Issue
- **Number**: #%d
- **Title**: %s
- **URL**: %s

## Workflow
1. Read the issue details: `+"`gh issue view %d`"+`
2. Understand the codebase and the issue
3. Implement the fix/feature
4. Run tests to verify
5. Stage your changes with `+"`git add`"+`
6. Generate a commit message by invoking the `+"`@flock-commit-writer`"+` subagent. Send it a message with the task context (issue #%d: %s), the output of `+"`git diff --cached`"+`, the list of staged files, and the worktree path (%s). It will return a properly formatted commit message. Make sure the commit message body includes `+"`Fixes #%d`"+`.
7. Commit with the generated message
8. Push the branch: `+"`git push -u origin %s`"+`
9. Create or update a PR by invoking the `+"`@flock-pr`"+` subagent. Send it the task context (issue #%d: %s), the output of `+"`git log --oneline -10`"+`, the issue URL (%s), and the worktree path (%s). It will create a new PR or update an existing one and return the PR URL.

## Environment
This project uses Nix for development tooling. To run commands with the devenv (compilers, linters, CLI utilities, etc.), wrap them with `+"`nix develop --impure -c bash -c \"<command>\"`"+`. For example: `+"`nix develop --impure -c bash -c \"go test ./...\"`"+`

## Rules
- Work autonomously — do not ask for human input
- Always work inside the worktree: `+"`%s`"+`
- Write clean, tested code
- If tests fail, fix them before proceeding
- Write progress to `+"`%s/.flock/memory/progress/issue_%d.md`"+`
- If you get stuck, describe the blocker in the progress file
- When done, do NOT remove the worktree — flock manages cleanup
`,
		workingDir,
		task.BranchName, worktreeDir,
		worktreeDir,
		worktreeDir,
		task.IssueNumber,
		task.Title,
		task.IssueUrl,
		task.IssueNumber,
		task.IssueNumber, task.Title,
		worktreeDir,
		task.IssueNumber,
		task.BranchName,
		task.IssueNumber, task.Title, task.IssueUrl,
		worktreeDir,
		worktreeDir,
		workingDir, task.IssueNumber,
	)
}

// RestartSubAgent creates a fresh session for a stuck task.
func RestartSubAgent(
	ctx context.Context,
	client *opencode.Client,
	queries *sqlc.Queries,
	instanceID string,
	workingDir string,
	task *sqlc.Task,
	reason string,
) error {
	// Read existing progress if any
	progressContent, _ := memory.ReadInstanceMemory(workingDir)

	session, err := client.CreateSession(ctx, workingDir)
	if err != nil {
		return fmt.Errorf("create restart session: %w", err)
	}

	if _, err := queries.CreateSession(ctx, sqlc.CreateSessionParams{
		ID:         session.ID,
		InstanceID: instanceID,
		Title:      fmt.Sprintf("Issue #%d (restart): %s", task.IssueNumber, task.Title),
		Status:     "active",
	}); err != nil {
		queries.UpsertSession(ctx, sqlc.UpsertSessionParams{
			ID:         session.ID,
			InstanceID: instanceID,
			Title:      fmt.Sprintf("Issue #%d (restart): %s", task.IssueNumber, task.Title),
			Status:     "active",
		})
	}

	queries.UpdateTaskSession(ctx, sqlc.UpdateTaskSessionParams{
		SessionID: session.ID,
		ID:        task.ID,
	})
	queries.UpdateTaskStatus(ctx, sqlc.UpdateTaskStatusParams{
		Status: "active",
		ID:     task.ID,
	})

	prompt := composeSubAgentPrompt(workingDir, task)
	prompt += fmt.Sprintf("\n## Previous Attempt\nThe previous session was stuck. Reason: %s\n", reason)
	prompt += fmt.Sprintf("\n## Note\nThe worktree and branch may already exist from the previous attempt. If `git worktree add` fails because it already exists, just `cd` into the worktree directory and continue from where the previous agent left off.\n")
	if progressContent != "" {
		prompt += fmt.Sprintf("\n## Context from Memory\n%s\n", progressContent)
	}

	log.Printf("agent: restarted sub-agent session %s for issue #%d (reason: %s)",
		session.ID[:8], task.IssueNumber, reason)

	// Send message in a goroutine so it doesn't block the heartbeat loop.
	go func() {
		if err := client.SendMessage(ctx, session.ID, prompt); err != nil {
			log.Printf("agent: restart message failed for issue #%d: %v", task.IssueNumber, err)
			queries.UpdateTaskStatus(context.Background(), sqlc.UpdateTaskStatusParams{
				Status: "failed",
				ID:     task.ID,
			})
		}
	}()

	return nil
}
