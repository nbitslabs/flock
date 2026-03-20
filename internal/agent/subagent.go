package agent

import (
	"context"
	"fmt"
	"log"

	"github.com/nbitslabs/flock/internal/db/sqlc"
	"github.com/nbitslabs/flock/internal/memory"
	"github.com/nbitslabs/flock/internal/opencode"
)

// CreateSubAgentSession creates a new OpenCode session inside the worktree and
// sends an issue-resolution prompt. The worktree must already exist (created by
// EnsureWorktree). The session and DB records are created synchronously, but the
// actual message send runs in a goroutine so it does not block the heartbeat loop.
func CreateSubAgentSession(
	ctx context.Context,
	client *opencode.Client,
	queries *sqlc.Queries,
	instanceID string,
	dataDir string,
	org, repo string,
	sourceRepoPath string,
	task *sqlc.Task,
) error {
	// Create the worktree via Go code (not LLM instructions)
	wtPath, err := EnsureWorktree(dataDir, org, repo, task.BranchName, sourceRepoPath)
	if err != nil {
		return fmt.Errorf("ensure worktree: %w", err)
	}

	// Create session inside the worktree directory
	session, err := client.CreateSession(ctx, wtPath)
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

	log.Printf("agent: created sub-agent session %s for issue #%d in worktree %s", session.ID[:8], task.IssueNumber, wtPath)

	prompt := composeSubAgentPrompt(dataDir, org, repo, wtPath, task)

	// Send message in a goroutine so it doesn't block the heartbeat loop.
	go func() {
		if err := client.SendMessage(ctx, session.ID, prompt, ""); err != nil {
			log.Printf("agent: sub-agent message failed for issue #%d: %v", task.IssueNumber, err)
			queries.UpdateTaskStatus(context.Background(), sqlc.UpdateTaskStatusParams{
				Status: "failed",
				ID:     task.ID,
			})
		}
	}()

	return nil
}

func composeSubAgentPrompt(dataDir, org, repo, worktreePath string, task *sqlc.Task) string {
	repoStatePath := memory.RepoStatePath(dataDir, org, repo)
	progressPath := memory.RepoProgressPath(dataDir, org, repo)

	return fmt.Sprintf(`You are an autonomous coding agent resolving a GitHub issue. Work independently to completion.

---
## ⚠ WORKTREE CONTEXT — READ BEFORE DOING ANYTHING ⚠

| Field | Value |
|-------|-------|
| **Working Directory** | `+"`%s`"+` |
| **Branch** | `+"`%s`"+` |
| **Issue** | #%d — %s |

### Worktree Restrictions
- You are inside a flock-managed git worktree. **Do NOT leave this directory.**
- **NEVER** run `+"`git checkout`"+`, `+"`git switch`"+`, or any `+"`git worktree`"+` command.
- **NEVER** remove, move, or modify this worktree — flock manages its lifecycle.
- All git commands (add, commit, push, status, diff, log) are allowed.

### Git Filter Setup
A git wrapper is installed that enforces these restrictions. Before running git commands, set your PATH:
`+"`"+`
export PATH="%s/.flock/bin:$PATH"
`+"`"+`
Run this export at the start of your session. All subsequent git commands will be filtered.

---

## Issue
- **Number**: #%d
- **Title**: %s
- **URL**: %s

## State Paths
- Repo state: `+"`%s`"+`
- Progress file: `+"`%s/issue_%d.md`"+`

## Workflow
1. Set up git filter: `+"`export PATH=\"%s/.flock/bin:$PATH\"`"+`
2. Read the issue details: `+"`gh issue view %d`"+`
3. Understand the codebase and the issue
4. Generate an implementation plan by invoking the `+"`@flock-issue-triage`"+` subagent. Send it the issue number, URL, title, and the repo state path. It will write a plan to `+"`%s/issue_%d.md`"+`.
5. Read the plan from `+"`%s/issue_%d.md`"+` to understand the proposed solution
6. Implement the fix/feature based on the plan
7. Run tests to verify
8. Stage your changes with `+"`git add`"+`
9. Generate a commit message by invoking the `+"`@flock-commit-writer`"+` subagent. Send it a message with the task context (issue #%d: %s), the output of `+"`git diff --cached`"+`, and the list of staged files. Make sure the commit message body includes `+"`Fixes #%d`"+`.
10. Commit with the generated message
11. Push the branch: `+"`git push -u origin %s`"+`
12. Create or update a PR by invoking the `+"`@flock-pr`"+` subagent. Send it the task context (issue #%d: %s), the output of `+"`git log --oneline -10`"+`, and the issue URL (%s).

## Development Environment
This project uses Nix for development tooling. To run commands with the devenv (compilers, linters, CLI utilities, etc.), wrap them with `+"`nix develop --impure -c bash -c \"<command>\"`"+`. For example: `+"`nix develop --impure -c bash -c \"go test ./...\"`"+`

## Rules
- Work autonomously — do not ask for human input
- Stay in your worktree directory: `+"`%s`"+`
- Your branch is `+"`%s`"+` — do NOT change it
- Write clean, tested code
- If tests fail, fix them before proceeding
- Write progress to `+"`%s/issue_%d.md`"+`
- If you get stuck, describe the blocker in the progress file
- When done, do NOT remove the worktree — flock manages cleanup
`,
		worktreePath,
		task.BranchName,
		task.IssueNumber, task.Title,
		worktreePath,
		task.IssueNumber,
		task.Title,
		task.IssueUrl,
		repoStatePath,
		progressPath, task.IssueNumber,
		worktreePath,
		task.IssueNumber,
		progressPath, task.IssueNumber,
		progressPath, task.IssueNumber,
		task.IssueNumber, task.Title,
		task.IssueNumber,
		task.BranchName,
		task.IssueNumber, task.Title, task.IssueUrl,
		worktreePath,
		task.BranchName,
		progressPath, task.IssueNumber,
	)
}

// composeWorktreeContextHeader returns the worktree context section that should
// be included at the top of every sub-agent message to reinforce boundaries.
func composeWorktreeContextHeader(worktreePath, branchName string) string {
	return fmt.Sprintf(`---
**WORKTREE REMINDER**: You are in `+"`%s`"+` on branch `+"`%s`"+`. Do NOT switch branches, leave this directory, or manage worktrees.
---
`, worktreePath, branchName)
}

// RestartSubAgent creates a fresh session for a stuck task.
func RestartSubAgent(
	ctx context.Context,
	client *opencode.Client,
	queries *sqlc.Queries,
	instanceID string,
	dataDir string,
	org, repo string,
	sourceRepoPath string,
	task *sqlc.Task,
	reason string,
) error {
	// Ensure worktree exists (may already exist from previous attempt)
	wtPath, err := EnsureWorktree(dataDir, org, repo, task.BranchName, sourceRepoPath)
	if err != nil {
		return fmt.Errorf("ensure worktree for restart: %w", err)
	}

	// Read existing progress if any
	progressContent, _ := memory.ReadRepoMemory(dataDir, org, repo)

	// Create session inside the worktree
	session, err := client.CreateSession(ctx, wtPath)
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

	prompt := composeSubAgentPrompt(dataDir, org, repo, wtPath, task)
	prompt += fmt.Sprintf("\n## Previous Attempt\nThe previous session was stuck. Reason: %s\n", reason)
	prompt += fmt.Sprintf("\n## Note\nThe worktree (%s) and branch (%s) already exist from the previous attempt. Continue from where the previous agent left off.\n",
		wtPath, task.BranchName)
	if progressContent != "" {
		prompt += fmt.Sprintf("\n## Context from Memory\n%s\n", progressContent)
	}
	prompt += "\n" + composeWorktreeContextHeader(wtPath, task.BranchName)

	log.Printf("agent: restarted sub-agent session %s for issue #%d (reason: %s)",
		session.ID[:8], task.IssueNumber, reason)

	go func() {
		if err := client.SendMessage(ctx, session.ID, prompt, ""); err != nil {
			log.Printf("agent: restart message failed for issue #%d: %v", task.IssueNumber, err)
			queries.UpdateTaskStatus(context.Background(), sqlc.UpdateTaskStatusParams{
				Status: "failed",
				ID:     task.ID,
			})
		}
	}()

	return nil
}
