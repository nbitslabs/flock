package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/nbitslabs/flock/internal/db/sqlc"
	"github.com/nbitslabs/flock/internal/memory"
	"github.com/nbitslabs/flock/internal/opencode"
)

// DecisionProcessor reads decision files written by the orchestrator and
// creates/restarts sub-agent sessions accordingly.
type DecisionProcessor struct {
	client     *opencode.Client
	queries    *sqlc.Queries
	workingDir string
	dataDir    string
}

func NewDecisionProcessor(client *opencode.Client, queries *sqlc.Queries, workingDir, dataDir string) *DecisionProcessor {
	return &DecisionProcessor{client: client, queries: queries, workingDir: workingDir, dataDir: dataDir}
}

// ProcessDecisions reads decision files from the working directory,
// creates sub-agent sessions for new tasks, marks completed tasks, and restarts stuck ones.
func (dp *DecisionProcessor) ProcessDecisions(ctx context.Context, instanceID, workingDir string) {
	dp.processCompletedTasks(ctx, instanceID, workingDir)
	dp.processNewTasks(ctx, instanceID, workingDir)
	dp.processRestarts(ctx, instanceID, workingDir)
	memory.ClearDecisionFiles(workingDir)
}

func (dp *DecisionProcessor) processNewTasks(ctx context.Context, instanceID, workingDir string) {
	decisions, err := memory.ReadNewTasks(workingDir)
	if err != nil {
		log.Printf("agent: failed to read new_tasks.json: %v", err)
		return
	}

	gh := NewGitHub(dp.dataDir)

	for _, d := range decisions {
		// Dedup: check if task already exists for this issue
		if _, err := dp.queries.GetTaskByIssue(ctx, sqlc.GetTaskByIssueParams{
			InstanceID:  instanceID,
			IssueNumber: int64(d.IssueNumber),
		}); err == nil {
			log.Printf("agent: task for issue #%d already exists, skipping", d.IssueNumber)
			continue
		}

		// React to the issue with 👀 when first detected
		if err := gh.ReactToIssue(ctx, d.IssueNumber, "eyes"); err != nil {
			log.Printf("agent: failed to react to issue #%d: %v", d.IssueNumber, err)
		}

		taskID := uuid.New().String()
		task, err := dp.queries.CreateTask(ctx, sqlc.CreateTaskParams{
			ID:          taskID,
			InstanceID:  instanceID,
			IssueNumber: int64(d.IssueNumber),
			IssueUrl:    d.IssueURL,
			Title:       d.Title,
			Status:      "pending",
			BranchName:  d.BranchName,
		})
		if err != nil {
			log.Printf("agent: failed to create task for issue #%d: %v", d.IssueNumber, err)
			continue
		}

		if err := CreateSubAgentSession(ctx, dp.client, dp.queries, instanceID, workingDir, &task); err != nil {
			log.Printf("agent: failed to create sub-agent for issue #%d: %v", d.IssueNumber, err)
			dp.queries.UpdateTaskStatus(ctx, sqlc.UpdateTaskStatusParams{
				Status: "failed",
				ID:     taskID,
			})
			continue
		}

		// Comment that we're looking at the issue with the branch name
		comment := fmt.Sprintf("I'm looking at this issue now. I'll be working on it in the `%s` branch.", d.BranchName)
		if err := gh.CommentOnIssue(ctx, d.IssueNumber, comment); err != nil {
			log.Printf("agent: failed to comment on issue #%d: %v", d.IssueNumber, err)
		}
	}
}

func (dp *DecisionProcessor) processCompletedTasks(ctx context.Context, instanceID, workingDir string) {
	decisions, err := memory.ReadCompletedTasks(workingDir)
	if err != nil {
		log.Printf("agent: failed to read completed_tasks.json: %v", err)
		return
	}

	for _, d := range decisions {
		task, err := dp.queries.GetTaskByID(ctx, d.TaskID)
		if err != nil {
			log.Printf("agent: failed to get task %s: %v", d.TaskID, err)
			continue
		}

		if err := dp.queries.UpdateTaskStatus(ctx, sqlc.UpdateTaskStatusParams{
			Status: "completed",
			ID:     d.TaskID,
		}); err != nil {
			log.Printf("agent: failed to mark task %s as completed: %v", d.TaskID, err)
			continue
		}

		if err := removeWorktree(workingDir, task.BranchName); err != nil {
			log.Printf("agent: failed to remove worktree for branch %s: %v", task.BranchName, err)
		}

		log.Printf("agent: marked task %s as completed (%s)", truncID(d.TaskID), d.Reason)
	}
}

func (dp *DecisionProcessor) processRestarts(ctx context.Context, instanceID, workingDir string) {
	decisions, err := memory.ReadRestartTasks(workingDir)
	if err != nil {
		log.Printf("agent: failed to read restart_tasks.json: %v", err)
		return
	}

	for _, d := range decisions {
		// Look up the task to get full details
		tasks, err := dp.queries.ListActiveTasks(ctx, instanceID)
		if err != nil {
			log.Printf("agent: failed to list active tasks: %v", err)
			return
		}

		for _, task := range tasks {
			if task.ID == d.TaskID {
				if err := RestartSubAgent(ctx, dp.client, dp.queries, instanceID, workingDir, &task, d.Reason); err != nil {
					log.Printf("agent: failed to restart task %s: %v", d.TaskID, err)
				}
				break
			}
		}
	}
}

func removeWorktree(workingDir, branchName string) error {
	worktreePath := filepath.Join(workingDir, ".flock", "worktrees", branchName)

	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return nil
	}

	cmd := exec.Command("git", "worktree", "remove", "--force", worktreePath)
	cmd.Dir = workingDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove failed: %w, output: %s", err, string(output))
	}

	log.Printf("agent: removed worktree at %s", worktreePath)
	return nil
}
