package agent

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/nbitslabs/flock/internal/db/sqlc"
	"github.com/nbitslabs/flock/internal/opencode"
)

// Scheduler runs the heartbeat loop for a single instance.
type Scheduler struct {
	instanceID    string
	dataDir       string
	cfg           AgentConfig
	orchestrator  *Orchestrator
	processor     *DecisionProcessor
	cleaner       *WorktreeCleaner
	healthChecker *HealthChecker
	queries       *sqlc.Queries
	client        *opencode.Client
	cancel        context.CancelFunc
	heartbeatNum  int // tracks heartbeat count for periodic health checks
}

func NewScheduler(
	instanceID, dataDir string,
	cfg AgentConfig,
	orchestrator *Orchestrator,
	processor *DecisionProcessor,
	cleaner *WorktreeCleaner,
	healthChecker *HealthChecker,
	queries *sqlc.Queries,
	client *opencode.Client,
) *Scheduler {
	return &Scheduler{
		instanceID:    instanceID,
		dataDir:       dataDir,
		cfg:           cfg,
		orchestrator:  orchestrator,
		processor:     processor,
		cleaner:       cleaner,
		healthChecker: healthChecker,
		queries:       queries,
		client:        client,
	}
}

// Start begins the heartbeat loop in a goroutine.
func (s *Scheduler) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	go s.run(ctx)
}

// Stop cancels the heartbeat loop.
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Scheduler) run(ctx context.Context) {
	interval := s.cfg.HeartbeatInterval
	if interval == 0 {
		interval = 5 * time.Minute
	}

	log.Printf("agent: scheduler started for instance %s (interval=%s)", truncID(s.instanceID), interval)

	// Run first heartbeat after a short delay to let things settle
	select {
	case <-time.After(10 * time.Second):
	case <-ctx.Done():
		return
	}

	s.doHeartbeat(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.doHeartbeat(ctx)
		case <-ctx.Done():
			log.Printf("agent: scheduler stopped for instance %s", truncID(s.instanceID))
			return
		}
	}
}

func (s *Scheduler) doHeartbeat(ctx context.Context) {
	log.Printf("agent: heartbeat for instance %s", truncID(s.instanceID))

	// 1. Send heartbeat to orchestrator and wait for response
	if err := s.orchestrator.SendHeartbeat(ctx); err != nil {
		log.Printf("agent: heartbeat send/wait failed for %s: %v", truncID(s.instanceID), err)
		// Don't return — the orchestrator may have already written decision
		// files before the error (e.g. waitForIdle timeout). Always attempt
		// to process them so work isn't silently dropped.
	}

	// 2. Process decision files (even if SendHeartbeat errored)
	s.processor.ProcessDecisions(ctx, s.instanceID)

	// 3. Check for stuck tasks and mark them
	s.checkStuckTasks(ctx)

	// 4. Run worktree cleanup for completed/abandoned tasks
	if s.cleaner != nil {
		results := s.cleaner.RunCleanup(ctx, s.instanceID)
		for _, r := range results {
			if r.Action == "failed" {
				log.Printf("agent: cleanup failed for task %s: %v", truncID(r.TaskID), r.Error)
			}
		}
	}

	// 5. Run health checks every 2nd heartbeat (roughly every 10 minutes
	// with default 5-minute interval)
	s.heartbeatNum++
	if s.healthChecker != nil && s.heartbeatNum%2 == 0 {
		results := s.healthChecker.RunHealthChecks(ctx)
		for _, r := range results {
			if r.Status == "corrupted" || r.Status == "missing" {
				log.Printf("agent: healthcheck: worktree %s is %s: %s",
					truncID(r.WorktreeID), r.Status, r.ErrorMessage)
			}
		}
	}
}

func (s *Scheduler) checkStuckTasks(ctx context.Context) {
	stuckTasks, err := s.queries.ListStuckTasks(ctx, sqlc.ListStuckTasksParams{
		InstanceID: s.instanceID,
		Column2:    fmt.Sprintf("%d", s.cfg.StuckThresholdSecs),
	})
	if err != nil {
		log.Printf("agent: failed to list stuck tasks: %v", err)
		return
	}

	for _, task := range stuckTasks {
		log.Printf("agent: task %s (issue #%d) is stuck (last activity: %s)",
			truncID(task.ID), task.IssueNumber, task.LastActivityAt)
		s.queries.UpdateTaskStatus(ctx, sqlc.UpdateTaskStatusParams{
			Status: "stuck",
			ID:     task.ID,
		})
	}
}
