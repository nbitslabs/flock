package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
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

	// Look up org/repo from DB
	instance, err := h.queries.GetInstance(h.ctx, instanceID)
	if err != nil {
		log.Printf("agent: failed to get instance %s: %v", truncID(instanceID), err)
		return
	}
	org := instance.Org
	repo := instance.Repo

	if org == "" || repo == "" {
		log.Printf("agent: instance %s has no org/repo set, skipping agent", truncID(instanceID))
		return
	}

	// Ensure global .flock directory layout
	if err := memory.EnsureLayout(h.dataDir); err != nil {
		log.Printf("agent: failed to ensure global layout: %v", err)
	}

	// Ensure repo-specific directory layout
	if err := memory.EnsureRepoLayout(h.dataDir, org, repo); err != nil {
		log.Printf("agent: failed to ensure repo layout for %s/%s: %v", org, repo, err)
	}

	// Attempt migration from old UUID-based paths
	h.migrateIfNeeded(instanceID, org, repo)

	// Check if heartbeat needs upgrade
	if err := h.upgradeHeartbeatIfNeeded(instanceID, org, repo); err != nil {
		log.Printf("agent: heartbeat upgrade failed for %s: %v", truncID(instanceID), err)
	}

	orch := NewOrchestrator(h.client, h.queries, instanceID, h.dataDir, org, repo, h.cfg, h.subscribeFn)
	proc := NewDecisionProcessor(h.client, h.queries, h.dataDir, instanceID, org, repo, h.cfg)
	cleaner := NewWorktreeCleaner(h.queries, h.dataDir, org, repo)
	sched := NewScheduler(instanceID, h.dataDir, h.cfg, orch, proc, cleaner, h.queries, h.client)
	sched.Start(h.ctx)

	h.schedulers[instanceID] = sched
	log.Printf("agent: started scheduler for instance %s (%s/%s)", truncID(instanceID), org, repo)
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

	h.tracker.RecordActivity(sessionID)
	h.updateTaskActivityForSession(sessionID)
}

func (h *Harness) updateTaskActivityForSession(sessionID string) {
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

// migrateIfNeeded checks if old UUID-based directories exist and migrates them.
func (h *Harness) migrateIfNeeded(instanceID, org, repo string) {
	oldInstanceDir := memory.InstanceMemoryPath(h.dataDir, instanceID)

	info, err := os.Stat(oldInstanceDir)
	if err != nil || !info.IsDir() {
		return
	}

	log.Printf("agent: migrating instance %s to %s/%s", truncID(instanceID), org, repo)
	if err := memory.MigrateInstanceToRepo(h.dataDir, instanceID, org, repo); err != nil {
		log.Printf("agent: migration failed for %s: %v", truncID(instanceID), err)
	} else {
		log.Printf("agent: migration completed for %s → %s/%s", truncID(instanceID), org, repo)
	}
}

// upgradeHeartbeatIfNeeded checks if the heartbeat template has been updated
// and upgrades it if necessary using OpenCode to merge the changes.
func (h *Harness) upgradeHeartbeatIfNeeded(instanceID, org, repo string) error {
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

	prompt, err := memory.HeartbeatUpgradePrompt(h.dataDir, org, repo)
	if err != nil {
		return fmt.Errorf("generate upgrade prompt: %w", err)
	}

	instance, err := h.queries.GetInstance(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("get instance: %w", err)
	}
	workingDir := instance.WorkingDirectory

	session, err := h.client.CreateSession(ctx, workingDir)
	if err != nil {
		return fmt.Errorf("create upgrade session: %w", err)
	}

	if err := h.client.SendMessage(ctx, session.ID, prompt, ""); err != nil {
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

	pollTicker := time.NewTicker(3 * time.Second)
	defer pollTicker.Stop()

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
		case <-pollTicker.C:
			idle, err := h.client.IsSessionIdle(ctx, session.ID)
			if err != nil {
				log.Printf("agent: poll session status failed: %v", err)
				continue
			}
			if idle {
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
