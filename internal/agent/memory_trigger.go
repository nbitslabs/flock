package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nbitslabs/flock/internal/db/sqlc"
	"github.com/nbitslabs/flock/internal/memory"
)

// MemoryTrigger checks for memory updates after task completion and creates
// follow-up tasks when memory reflection is missing.
type MemoryTrigger struct {
	queries    *sqlc.Queries
	dataDir    string
	org        string
	repo       string
	instanceID string
}

// NewMemoryTrigger creates a new MemoryTrigger.
func NewMemoryTrigger(queries *sqlc.Queries, dataDir, instanceID, org, repo string) *MemoryTrigger {
	return &MemoryTrigger{
		queries:    queries,
		dataDir:    dataDir,
		org:        org,
		repo:       repo,
		instanceID: instanceID,
	}
}

// MemoryCheckResult records the result of checking whether memory was updated.
type MemoryCheckResult struct {
	TaskID       string
	IssueNumber  int64
	MemoryFound  bool
	NeedsUpdate  bool
	FollowUpSent bool
}

// CheckCompletedTasks verifies that completed tasks have corresponding memory
// updates. If memory is missing, it creates a follow-up instruction for the
// orchestrator.
func (mt *MemoryTrigger) CheckCompletedTasks(ctx context.Context) []MemoryCheckResult {
	tasks, err := mt.queries.ListCompletedTasks(ctx, mt.instanceID)
	if err != nil {
		log.Printf("agent: memory-trigger: failed to list completed tasks: %v", err)
		return nil
	}

	var results []MemoryCheckResult
	for _, task := range tasks {
		result := mt.checkTaskMemory(ctx, task)
		results = append(results, result)
	}

	return results
}

// checkTaskMemory checks if memory was updated for a specific completed task.
func (mt *MemoryTrigger) checkTaskMemory(ctx context.Context, task sqlc.Task) MemoryCheckResult {
	result := MemoryCheckResult{
		TaskID:      task.ID,
		IssueNumber: task.IssueNumber,
	}

	// Check if memory was updated after task completion
	completedAt, err := time.Parse("2006-01-02 15:04:05", task.UpdatedAt)
	if err != nil {
		completedAt, _ = time.Parse(time.RFC3339, task.UpdatedAt)
	}

	// Only check tasks completed within the last 24 hours
	if time.Since(completedAt) > 24*time.Hour {
		return result
	}

	// Check for memory files related to this issue
	repoStatePath := memory.RepoStatePath(mt.dataDir, mt.org, mt.repo)
	memoryPath := filepath.Join(repoStatePath, "MEMORY.md")

	memContent, err := os.ReadFile(memoryPath)
	if err != nil {
		result.NeedsUpdate = true
		return result
	}

	// Check if the memory file references this issue
	issueRef := fmt.Sprintf("#%d", task.IssueNumber)
	if strings.Contains(string(memContent), issueRef) {
		result.MemoryFound = true
		return result
	}

	// Check reflection directory for issue-specific reflections
	reflectionDir := memory.ReflectionPath(mt.dataDir)
	if entries, err := os.ReadDir(reflectionDir); err == nil {
		issuePattern := fmt.Sprintf("issue_%d", task.IssueNumber)
		for _, entry := range entries {
			if strings.Contains(entry.Name(), issuePattern) {
				result.MemoryFound = true
				return result
			}
		}
	}

	// Memory not found — needs update
	result.NeedsUpdate = true

	// Write a follow-up reminder in the decisions directory
	if mt.writeMemoryReminder(task) {
		result.FollowUpSent = true
		log.Printf("agent: memory-trigger: created memory update reminder for issue #%d", task.IssueNumber)
	}

	return result
}

// writeMemoryReminder writes a reminder file that the orchestrator will pick up
// during the next heartbeat, instructing it to trigger memory reflection.
func (mt *MemoryTrigger) writeMemoryReminder(task sqlc.Task) bool {
	decisionsPath := memory.RepoDecisionsPath(mt.dataDir, mt.org, mt.repo)
	reminderPath := filepath.Join(decisionsPath, fmt.Sprintf("memory_reminder_%d.md", task.IssueNumber))

	// Don't create duplicate reminders
	if _, err := os.Stat(reminderPath); err == nil {
		return false
	}

	content := fmt.Sprintf(`# Memory Update Required

Issue #%d (%s) was completed but no memory update was found.

## Action Required
Invoke the @flock-self-reflect subagent to update memory for this issue.
Send it the following context:
- Issue: #%d — %s
- Branch: %s
- PR: %s
- Task ID: %s

The self-reflection should capture:
1. What was learned during implementation
2. Any patterns or conventions discovered
3. Technical decisions made and why
4. Dependencies or gotchas encountered
`,
		task.IssueNumber, task.Title,
		task.IssueNumber, task.Title,
		task.BranchName,
		task.PrUrl,
		task.ID,
	)

	if err := os.WriteFile(reminderPath, []byte(content), 0o644); err != nil {
		log.Printf("agent: memory-trigger: failed to write reminder for issue #%d: %v", task.IssueNumber, err)
		return false
	}

	return true
}
