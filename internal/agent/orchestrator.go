package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nbitslabs/flock/internal/db/sqlc"
	"github.com/nbitslabs/flock/internal/memory"
	"github.com/nbitslabs/flock/internal/opencode"
)

// Orchestrator manages the orchestrator session for a single instance.
type Orchestrator struct {
	client     *opencode.Client
	queries    *sqlc.Queries
	instanceID string
	workingDir string
	cfg        AgentConfig
	// subscribeFn returns a channel of raw SSE events and an unsubscribe func.
	subscribeFn func(sessionID string) (<-chan string, func())
}

func NewOrchestrator(
	client *opencode.Client,
	queries *sqlc.Queries,
	instanceID, workingDir string,
	cfg AgentConfig,
	subscribeFn func(sessionID string) (<-chan string, func()),
) *Orchestrator {
	return &Orchestrator{
		client:      client,
		queries:     queries,
		instanceID:  instanceID,
		workingDir:  workingDir,
		cfg:         cfg,
		subscribeFn: subscribeFn,
	}
}

// EnsureSession returns the active orchestrator session, creating or rotating
// as needed. The boolean return indicates whether a new session was just created
// (and bootstrapped), so the caller can skip sending a duplicate heartbeat.
func (o *Orchestrator) EnsureSession(ctx context.Context) (*sqlc.OrchestratorSession, bool, error) {
	orch, err := o.queries.GetActiveOrchestratorSession(ctx, o.instanceID)
	if err == nil {
		// Check if rotation is needed
		if int(orch.HeartbeatCount) >= o.cfg.MaxHeartbeatsPerSession {
			log.Printf("agent: rotating orchestrator session (heartbeats=%d)", orch.HeartbeatCount)
			o.queries.RetireOrchestratorSession(ctx, orch.ID)
			sess, err := o.createOrchestratorSession(ctx)
			return sess, true, err
		}
		return &orch, false, nil
	}

	// No active session — create one
	sess, err := o.createOrchestratorSession(ctx)
	return sess, true, err
}

func (o *Orchestrator) createOrchestratorSession(ctx context.Context) (*sqlc.OrchestratorSession, error) {
	session, err := o.client.CreateSession(ctx)
	if err != nil {
		return nil, fmt.Errorf("create orchestrator session: %w", err)
	}

	// Store in sessions table too
	o.queries.UpsertSession(ctx, sqlc.UpsertSessionParams{
		ID:         session.ID,
		InstanceID: o.instanceID,
		Title:      "Orchestrator",
		Status:     "active",
	})

	orchID := uuid.New().String()
	orch, err := o.queries.CreateOrchestratorSession(ctx, sqlc.CreateOrchestratorSessionParams{
		ID:         orchID,
		InstanceID: o.instanceID,
		SessionID:  session.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("store orchestrator session: %w", err)
	}

	// Bootstrap with memory context
	bootstrapMsg := o.composeBootstrapMessage()
	if bootstrapMsg != "" {
		if err := o.client.SendMessage(ctx, session.ID, bootstrapMsg); err != nil {
			log.Printf("agent: failed to send bootstrap message: %v", err)
		} else {
			// Wait for bootstrap to complete
			o.waitForIdle(ctx, session.ID)
		}
	}

	log.Printf("agent: created orchestrator session %s (opencode session %s)",
		orchID[:8], session.ID[:8])
	return &orch, nil
}

func (o *Orchestrator) composeBootstrapMessage() string {
	var sb strings.Builder

	heartbeat, err := memory.ReadHeartbeat(o.workingDir)
	if err == nil && heartbeat != "" {
		sb.WriteString("# Heartbeat Instructions\n\n")
		sb.WriteString(heartbeat)
		sb.WriteString("\n\n")
	}

	instMemory, err := memory.ReadInstanceMemory(o.workingDir)
	if err == nil && instMemory != "" {
		sb.WriteString("# Instance Memory\n\n")
		sb.WriteString(instMemory)
		sb.WriteString("\n\n")
	}

	if sb.Len() == 0 {
		return ""
	}

	return fmt.Sprintf("You are the orchestrator AI. Read and internalize these instructions. "+
		"You will receive periodic heartbeat messages. Your working directory is: %s\n\n%s"+
		"Acknowledge that you understand your role.", o.workingDir, sb.String())
}

// SendHeartbeat sends a heartbeat message to the orchestrator session and
// waits for it to become idle. If the session was just created (bootstrapped),
// it skips sending a duplicate heartbeat since the bootstrap already triggered
// the orchestrator to check for issues and write decision files.
func (o *Orchestrator) SendHeartbeat(ctx context.Context) error {
	orch, justCreated, err := o.EnsureSession(ctx)
	if err != nil {
		return fmt.Errorf("ensure session: %w", err)
	}

	if justCreated {
		// Bootstrap already had the orchestrator check for issues and write
		// decision files. Sending another heartbeat now would cause it to
		// overwrite those files with empty arrays. Skip and let the caller
		// process the bootstrap's decisions.
		log.Printf("agent: session just bootstrapped, skipping duplicate heartbeat")
		return nil
	}

	msg := o.composeHeartbeatMessage(ctx)

	if err := o.client.SendMessage(ctx, orch.SessionID, msg); err != nil {
		return fmt.Errorf("send heartbeat: %w", err)
	}

	o.queries.IncrementHeartbeatCount(ctx, orch.ID)

	// Wait for orchestrator to finish processing
	if err := o.waitForIdle(ctx, orch.SessionID); err != nil {
		return fmt.Errorf("wait for idle: %w", err)
	}

	return nil
}

func (o *Orchestrator) composeHeartbeatMessage(ctx context.Context) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Heartbeat\n\nWorking directory: `%s`\n\n", o.workingDir))

	// Include tracked tasks
	tasks, err := o.queries.ListActiveTasks(ctx, o.instanceID)
	if err == nil && len(tasks) > 0 {
		sb.WriteString("## Active Tasks\n\n")
		for _, t := range tasks {
			sb.WriteString(fmt.Sprintf("- **#%d** %s (status: %s, session: %s, branch: %s)\n",
				t.IssueNumber, t.Title, t.Status, truncID(t.SessionID), t.BranchName))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("## Active Tasks\nNo active tasks.\n\n")
	}

	// Include stuck tasks
	stuckTasks, err := o.queries.ListStuckTasks(ctx, sqlc.ListStuckTasksParams{
		InstanceID: o.instanceID,
		Column2:    fmt.Sprintf("%d", o.cfg.StuckThresholdSecs),
	})
	if err == nil && len(stuckTasks) > 0 {
		sb.WriteString("## Stuck Tasks (no activity)\n\n")
		for _, t := range stuckTasks {
			sb.WriteString(fmt.Sprintf("- **#%d** %s (last activity: %s, task_id: %s)\n",
				t.IssueNumber, t.Title, t.LastActivityAt, t.ID))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Instructions\n")
	sb.WriteString("1. Run `gh issue list --assignee=@me --state=open --json number,url,title`\n")
	sb.WriteString("2. Compare with active tasks above\n")
	sb.WriteString("3. Write `.flock/memory/new_tasks.json` for new issues\n")
	sb.WriteString("4. Write `.flock/memory/restart_tasks.json` for stuck tasks needing restart\n")
	sb.WriteString("5. Update `.flock/memory/MEMORY.md` with any observations\n")

	return sb.String()
}

// waitForIdle subscribes to SSE events for the given session and blocks until
// a session.idle event is received or the timeout is hit.
func (o *Orchestrator) waitForIdle(ctx context.Context, sessionID string) error {
	if o.subscribeFn == nil {
		// No subscriber available — just wait a fixed time
		time.Sleep(30 * time.Second)
		return nil
	}

	ch, unsub := o.subscribeFn(sessionID)
	defer unsub()

	timeout := o.cfg.WaitForIdleTimeout
	if timeout == 0 {
		timeout = 3 * time.Minute
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case raw, ok := <-ch:
			if !ok {
				return nil
			}
			// Parse event type from the SSE data line
			data := strings.TrimPrefix(raw, "data: ")
			data = strings.TrimSpace(data)
			var env struct {
				Type string `json:"type"`
			}
			if json.Unmarshal([]byte(data), &env) == nil && env.Type == "session.idle" {
				return nil
			}
		case <-timer.C:
			return fmt.Errorf("timeout waiting for session idle")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func truncID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
