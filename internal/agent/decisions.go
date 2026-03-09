package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"

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
	dataDir    string
	instanceID string
	cfg        AgentConfig
}

func NewDecisionProcessor(client *opencode.Client, queries *sqlc.Queries, dataDir, instanceID string, cfg AgentConfig) *DecisionProcessor {
	return &DecisionProcessor{client: client, queries: queries, dataDir: dataDir, instanceID: instanceID, cfg: cfg}
}

// ProcessDecisions reads decision files from the instance memory directory,
// creates sub-agent sessions for new tasks, marks completed tasks, and restarts stuck ones.
func (dp *DecisionProcessor) ProcessDecisions(ctx context.Context, instanceID string) {
	dp.processCompletedTasks(ctx, instanceID)
	dp.processNewTasks(ctx, instanceID)
	dp.processRestarts(ctx, instanceID)
	memory.ClearDecisionFiles(dp.dataDir, instanceID)
}

func (dp *DecisionProcessor) processNewTasks(ctx context.Context, instanceID string) {
	decisions, err := memory.ReadNewTasks(dp.dataDir, instanceID)
	if err != nil {
		log.Printf("agent: failed to read new_tasks.json: %v", err)
		return
	}

	for _, d := range decisions {
		// Dedup: check if task already exists for this issue
		if _, err := dp.queries.GetTaskByIssue(ctx, sqlc.GetTaskByIssueParams{
			InstanceID:  instanceID,
			IssueNumber: int64(d.IssueNumber),
		}); err == nil {
			log.Printf("agent: task for issue #%d already exists, skipping", d.IssueNumber)
			continue
		}

		// Check session limits before creating a new task
		if dp.cfg.MaxParallelSessions > 0 || dp.cfg.MaxParallelSessionsPerInst > 0 {
			instanceCount, err := dp.queries.CountActiveTasksByInstance(ctx, instanceID)
			if err != nil {
				log.Printf("agent: failed to count instance active tasks: %v", err)
				continue
			}

			if dp.cfg.MaxParallelSessionsPerInst > 0 && int(instanceCount) >= dp.cfg.MaxParallelSessionsPerInst {
				log.Printf("agent: max parallel sessions per instance (%d) reached for %s, skipping issue #%d",
					dp.cfg.MaxParallelSessionsPerInst, instanceID[:8], d.IssueNumber)
				continue
			}

			if dp.cfg.MaxParallelSessions > 0 {
				overallCount, err := dp.queries.CountAllActiveTasks(ctx)
				if err != nil {
					log.Printf("agent: failed to count overall active tasks: %v", err)
					continue
				}

				if int(overallCount) >= dp.cfg.MaxParallelSessions {
					log.Printf("agent: max parallel sessions (%d) reached, skipping issue #%d",
						dp.cfg.MaxParallelSessions, d.IssueNumber)
					continue
				}
			}
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

		// Get workingDir from instance
		instance, err := dp.queries.GetInstance(ctx, instanceID)
		workingDir := ""
		if err == nil {
			workingDir = instance.WorkingDirectory
		}

		if err := CreateSubAgentSession(ctx, dp.client, dp.queries, instanceID, dp.dataDir, workingDir, &task); err != nil {
			log.Printf("agent: failed to create sub-agent for issue #%d: %v", d.IssueNumber, err)
			dp.queries.UpdateTaskStatus(ctx, sqlc.UpdateTaskStatusParams{
				Status: "failed",
				ID:     taskID,
			})
			continue
		}
	}
}

func (dp *DecisionProcessor) processCompletedTasks(ctx context.Context, instanceID string) {
	decisions, err := memory.ReadCompletedTasks(dp.dataDir, instanceID)
	if err != nil {
		log.Printf("agent: failed to read completed_tasks.json: %v", err)
		return
	}

	for _, d := range decisions {
		if _, err := dp.queries.GetTaskByID(ctx, d.TaskID); err != nil {
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

		log.Printf("agent: marked task %s as completed (%s)", truncID(d.TaskID), d.Reason)
	}
}

func (dp *DecisionProcessor) processRestarts(ctx context.Context, instanceID string) {
	decisions, err := memory.ReadRestartTasks(dp.dataDir, instanceID)
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
				// Get workingDir from instance
				instance, err := dp.queries.GetInstance(ctx, instanceID)
				workingDir := ""
				if err == nil {
					workingDir = instance.WorkingDirectory
				}

				if err := RestartSubAgent(ctx, dp.client, dp.queries, instanceID, dp.dataDir, workingDir, &task, d.Reason); err != nil {
					log.Printf("agent: failed to restart task %s: %v", d.TaskID, err)
				}
				break
			}
		}
	}
}

func removeWorktree(dataDir, instanceID, branchName string) error {
	worktreePath := memory.InstanceWorktreePath(dataDir, instanceID, branchName)

	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return nil
	}

	// Get workingDir from instance to use as the base for git worktree remove
	// We need to find a valid git repo to run the command
	cmd := exec.Command("git", "worktree", "remove", "--force", worktreePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove failed: %w, output: %s", err, string(output))
	}

	log.Printf("agent: removed worktree at %s", worktreePath)
	return nil
}
