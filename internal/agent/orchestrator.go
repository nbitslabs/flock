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
	dataDir    string
	org        string
	repo       string
	cfg        AgentConfig
	// subscribeFn returns a channel of raw SSE events and an unsubscribe func.
	subscribeFn func(sessionID string) (<-chan string, func())
}

func NewOrchestrator(
	client *opencode.Client,
	queries *sqlc.Queries,
	instanceID, dataDir string,
	org, repo string,
	cfg AgentConfig,
	subscribeFn func(sessionID string) (<-chan string, func()),
) *Orchestrator {
	return &Orchestrator{
		client:      client,
		queries:     queries,
		instanceID:  instanceID,
		dataDir:     dataDir,
		org:         org,
		repo:        repo,
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
		// Verify the session still exists in OpenCode
		if _, err := o.client.GetSession(ctx, orch.SessionID); err != nil {
			log.Printf("agent: orchestrator session %s no longer exists in opencode, retiring", truncID(orch.SessionID))
			o.queries.RetireOrchestratorSession(ctx, orch.ID)
			sess, err := o.createOrchestratorSession(ctx)
			return sess, true, err
		}

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
	// Get workingDir from the database
	instance, err := o.queries.GetInstance(ctx, o.instanceID)
	if err != nil {
		return nil, fmt.Errorf("get instance: %w", err)
	}
	workingDir := instance.WorkingDirectory

	session, err := o.client.CreateSession(ctx, workingDir)
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
		if err := o.client.SendMessage(ctx, session.ID, bootstrapMsg, ""); err != nil {
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
	repoStatePath := memory.RepoStatePath(o.dataDir, o.org, o.repo)
	decisionsPath := memory.RepoDecisionsPath(o.dataDir, o.org, o.repo)

	instance, err := o.queries.GetInstance(context.Background(), o.instanceID)
	workingDir := ""
	if err == nil {
		workingDir = instance.WorkingDirectory
	}

	return fmt.Sprintf("You are the orchestrator for this repo. You will receive periodic heartbeat messages.\n\n"+
		"Paths: working_dir=`%s` state=`%s` decisions=`%s`\n\n"+
		"Read your instructions: `cat %s/HEARTBEAT.md`\n"+
		"Read repo memory: `cat %s/MEMORY.md`\n\n"+
		"Agents: `@flock-history-analyzer` (history queries), `@flock-self-reflect` (post-completion), `@flock-implementation-agent` (issue resolution).\n\n"+
		"Read HEARTBEAT.md now, then acknowledge.",
		workingDir, repoStatePath, decisionsPath, repoStatePath, repoStatePath)
}

// SendHeartbeat sends a heartbeat message to the orchestrator session and
// waits for it to become idle.
func (o *Orchestrator) SendHeartbeat(ctx context.Context) error {
	orch, justCreated, err := o.EnsureSession(ctx)
	if err != nil {
		return fmt.Errorf("ensure session: %w", err)
	}

	// Always send a heartbeat message, even if the session was just bootstrapped.
	// The orchestrator needs to receive heartbeat instructions to write decision
	// files (new_tasks.json, etc.). Without this, ProcessDecisions would run
	// immediately but find no decision files to process.
	msg := o.composeHeartbeatMessage(ctx)

	if err := o.client.SendMessage(ctx, orch.SessionID, msg, ""); err != nil {
		log.Printf("agent: heartbeat send failed, retiring stale session %s: %v", truncID(orch.SessionID), err)
		o.queries.RetireOrchestratorSession(ctx, orch.ID)
		orch, _, err = o.EnsureSession(ctx)
		if err != nil {
			return fmt.Errorf("recreate session after send failure: %w", err)
		}
		// Retry sending heartbeat to the new session
		msg = o.composeHeartbeatMessage(ctx)
		if err := o.client.SendMessage(ctx, orch.SessionID, msg, ""); err != nil {
			return fmt.Errorf("retry heartbeat after recreation: %w", err)
		}
	}

	// Only increment heartbeat count for non-bootstrap heartbeats.
	// When justCreated is true, this is the bootstrap heartbeat, not a regular one.
	if !justCreated {
		o.queries.IncrementHeartbeatCount(ctx, orch.ID)
	}

	if err := o.waitForIdle(ctx, orch.SessionID); err != nil {
		return fmt.Errorf("wait for idle: %w", err)
	}

	return nil
}

func (o *Orchestrator) composeHeartbeatMessage(ctx context.Context) string {
	decisionsPath := memory.RepoDecisionsPath(o.dataDir, o.org, o.repo)

	activeCount, _ := o.queries.CountActiveTasksByInstance(ctx, o.instanceID)
	stuckTasks, _ := o.queries.ListStuckTasks(ctx, sqlc.ListStuckTasksParams{
		InstanceID: o.instanceID,
		Column2:    fmt.Sprintf("%d", o.cfg.StuckThresholdSecs),
	})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Heartbeat: active=%d stuck=%d decisions=`%s`\n", activeCount, len(stuckTasks), decisionsPath))

	if len(stuckTasks) > 0 {
		sb.WriteString("Stuck:")
		for _, t := range stuckTasks {
			sb.WriteString(fmt.Sprintf(" #%d(%s,last:%s)", t.IssueNumber, t.ID, t.LastActivityAt))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Run your HEARTBEAT.md steps now.")

	return sb.String()
}

// waitForIdle blocks until the session becomes idle.
func (o *Orchestrator) waitForIdle(ctx context.Context, sessionID string) error {
	timeout := o.cfg.WaitForIdleTimeout
	if timeout == 0 {
		timeout = 3 * time.Minute
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	pollTicker := time.NewTicker(3 * time.Second)
	defer pollTicker.Stop()

	var ch <-chan string
	if o.subscribeFn != nil {
		var unsub func()
		ch, unsub = o.subscribeFn(sessionID)
		defer unsub()
	}

	for {
		select {
		case raw, ok := <-ch:
			if !ok {
				return nil
			}
			data := strings.TrimPrefix(raw, "data: ")
			data = strings.TrimSpace(data)
			var env struct {
				Type string `json:"type"`
			}
			if json.Unmarshal([]byte(data), &env) == nil && env.Type == "session.idle" {
				return nil
			}
		case <-pollTicker.C:
			idle, err := o.client.IsSessionIdle(ctx, sessionID)
			if err != nil {
				log.Printf("agent: poll session status failed: %v", err)
				continue
			}
			if idle {
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
