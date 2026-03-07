package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/nbitslabs/flock/internal/db/sqlc"
	"github.com/nbitslabs/flock/internal/memory"
	"github.com/nbitslabs/flock/internal/opencode"
)

// Harness is the top-level agent harness that manages per-instance schedulers.
type Harness struct {
	mu         sync.RWMutex
	schedulers map[string]*Scheduler // instanceID -> scheduler
	tracker    *ActivityTracker
	client     *opencode.Client
	queries    *sqlc.Queries
	cfg        AgentConfig
	ctx        context.Context
	cancel     context.CancelFunc
	// subscribeFn is set by the caller to provide internal SSE subscriptions.
	subscribeFn func(sessionID string) (<-chan string, func())
	dataDir     string
}

// NewHarness creates a new agent harness.
func NewHarness(
	client *opencode.Client,
	queries *sqlc.Queries,
	cfg AgentConfig,
	subscribeFn func(sessionID string) (<-chan string, func()),
	dataDir string,
) *Harness {
	cfg.Resolve()
	return &Harness{
		schedulers:  make(map[string]*Scheduler),
		tracker:     NewActivityTracker(),
		client:      client,
		queries:     queries,
		cfg:         cfg,
		subscribeFn: subscribeFn,
		dataDir:     dataDir,
	}
}

// Start initializes the harness context.
func (h *Harness) Start() {
	h.ctx, h.cancel = context.WithCancel(context.Background())
	log.Printf("agent: harness started (enabled=%v, interval=%s)", h.cfg.Enabled, h.cfg.HeartbeatInterval)
}

// Stop cancels all schedulers and the harness context.
func (h *Harness) Stop() {
	h.mu.Lock()
	for id, sched := range h.schedulers {
		sched.Stop()
		delete(h.schedulers, id)
	}
	h.mu.Unlock()

	if h.cancel != nil {
		h.cancel()
	}
	log.Println("agent: harness stopped")
}

// StartInstance begins the heartbeat loop for an instance.
func (h *Harness) StartInstance(instanceID, workingDir string) {
	if !h.cfg.Enabled {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.schedulers[instanceID]; exists {
		return
	}

	// Ensure .flock directory layout
	if err := memory.EnsureLayout(workingDir); err != nil {
		log.Printf("agent: failed to ensure layout for %s: %v", truncID(instanceID), err)
	}

	// Check if heartbeat needs upgrade
	if err := h.upgradeHeartbeatIfNeeded(instanceID, workingDir); err != nil {
		log.Printf("agent: heartbeat upgrade failed for %s: %v", truncID(instanceID), err)
	}

	orch := NewOrchestrator(h.client, h.queries, instanceID, workingDir, h.cfg, h.subscribeFn)
	proc := NewDecisionProcessor(h.client, h.queries, workingDir, h.dataDir)
	sched := NewScheduler(instanceID, workingDir, h.cfg, orch, proc, h.queries, h.client)
	sched.Start(h.ctx)

	h.schedulers[instanceID] = sched
	log.Printf("agent: started scheduler for instance %s", truncID(instanceID))
}

// StopInstance stops the heartbeat loop for an instance.
func (h *Harness) StopInstance(instanceID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if sched, ok := h.schedulers[instanceID]; ok {
		sched.Stop()
		delete(h.schedulers, instanceID)
		log.Printf("agent: stopped scheduler for instance %s", truncID(instanceID))
	}
}

// HandleEvent processes a raw SSE event to update activity tracking.
// Should be called from the event handler chain in main.go.
func (h *Harness) HandleEvent(instanceID, rawJSON string) {
	var env struct {
		Type       string `json:"type"`
		Properties struct {
			SessionID string `json:"sessionID"`
			Info      struct {
				SessionID string `json:"sessionID"`
			} `json:"info"`
			Part struct {
				SessionID string `json:"sessionID"`
			} `json:"part"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &env); err != nil {
		return
	}

	sessionID := env.Properties.SessionID
	if sessionID == "" {
		sessionID = env.Properties.Info.SessionID
	}
	if sessionID == "" {
		sessionID = env.Properties.Part.SessionID
	}
	if sessionID == "" {
		return
	}

	// Record activity for stuck detection
	h.tracker.RecordActivity(sessionID)

	// Update task activity in DB for any session that has a task
	h.updateTaskActivityForSession(sessionID)
}

func (h *Harness) updateTaskActivityForSession(sessionID string) {
	// Find tasks with this session ID and update their activity
	h.mu.RLock()
	instanceIDs := make([]string, 0, len(h.schedulers))
	for id := range h.schedulers {
		instanceIDs = append(instanceIDs, id)
	}
	h.mu.RUnlock()

	ctx := context.Background()
	for _, instID := range instanceIDs {
		tasks, err := h.queries.ListActiveTasks(ctx, instID)
		if err != nil {
			continue
		}
		for _, task := range tasks {
			if task.SessionID == sessionID {
				h.queries.UpdateTaskActivity(ctx, task.ID)
				return
			}
		}
	}
}

// Enabled returns whether the agent harness is enabled.
func (h *Harness) Enabled() bool {
	return h.cfg.Enabled
}

// upgradeHeartbeatIfNeeded checks if the heartbeat template has been updated
// and upgrades it if necessary using OpenCode to merge the changes.
func (h *Harness) upgradeHeartbeatIfNeeded(instanceID, workingDir string) error {
	ctx := context.Background()
	currentHash := memory.TemplateHash()

	storedHash, err := h.queries.GetInstanceHeartbeatHash(ctx, instanceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			storedHash = ""
		} else {
			return fmt.Errorf("get heartbeat hash: %w", err)
		}
	}

	if storedHash == currentHash {
		return nil
	}

	if storedHash == "" {
		log.Printf("agent: storing initial heartbeat hash for instance %s", truncID(instanceID))
		h.queries.UpdateInstanceHeartbeatHash(ctx, sqlc.UpdateInstanceHeartbeatHashParams{
			HeartbeatHash: currentHash,
			ID:            instanceID,
		})
		return nil
	}

	log.Printf("agent: heartbeat template changed for instance %s, upgrading", truncID(instanceID))

	prompt, err := memory.HeartbeatUpgradePrompt(workingDir)
	if err != nil {
		return fmt.Errorf("generate upgrade prompt: %w", err)
	}

	session, err := h.client.CreateSession(ctx)
	if err != nil {
		return fmt.Errorf("create upgrade session: %w", err)
	}

	if err := h.client.SendMessage(ctx, session.ID, prompt); err != nil {
		return fmt.Errorf("send upgrade prompt: %w", err)
	}

	ch, unsub := h.subscribeFn(session.ID)
	defer unsub()

	timeout := h.cfg.WaitForIdleTimeout
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
			data := strings.TrimPrefix(raw, "data: ")
			data = strings.TrimSpace(data)
			var env struct {
				Type string `json:"type"`
			}
			if json.Unmarshal([]byte(data), &env) == nil && env.Type == "session.idle" {
				log.Printf("agent: heartbeat upgrade completed for instance %s", truncID(instanceID))
				h.queries.UpdateInstanceHeartbeatHash(ctx, sqlc.UpdateInstanceHeartbeatHashParams{
					HeartbeatHash: currentHash,
					ID:            instanceID,
				})
				return nil
			}
		case <-timer.C:
			return fmt.Errorf("timeout waiting for heartbeat upgrade")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
