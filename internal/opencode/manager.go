package opencode

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nbitslabs/flock/internal/db/sqlc"
)

// Instance represents a registered OpenCode working directory.
type Instance struct {
	ID               string
	WorkingDirectory string
	Org              string
	Repo             string
	Status           string
	Client           *Client
}

// EventHandler is called when an SSE event arrives from the OpenCode server.
type EventHandler func(instanceID, rawJSON string)

// InstanceHook is called when an instance is registered or stopped.
// action is "register" or "stop".
type InstanceHook func(action string, inst *Instance)

// Manager manages OpenCode instances as logical records backed by a shared
// external OpenCode server. It does not spawn or manage OS processes.
type Manager struct {
	mu           sync.RWMutex
	instances    map[string]*Instance
	queries      *sqlc.Queries
	eventHandler EventHandler
	client       *Client
	cancelEvents context.CancelFunc
	instanceHook InstanceHook
}

// SetInstanceHook sets a callback invoked on instance register/stop.
func (m *Manager) SetInstanceHook(hook InstanceHook) {
	m.mu.Lock()
	m.instanceHook = hook
	m.mu.Unlock()
}

// NewManager creates a Manager that uses the given shared Client to talk to
// the external OpenCode server.
func NewManager(queries *sqlc.Queries, handler EventHandler, client *Client) *Manager {
	return &Manager{
		instances:    make(map[string]*Instance),
		queries:      queries,
		eventHandler: handler,
		client:       client,
	}
}

// StartEventSubscription begins a single global SSE subscription to the
// OpenCode server's /event endpoint. Events are routed through the handler.
func (m *Manager) StartEventSubscription() {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelEvents = cancel
	go m.subscribeEvents(ctx)
}

// StopEventSubscription cancels the global SSE subscription.
func (m *Manager) StopEventSubscription() {
	if m.cancelEvents != nil {
		m.cancelEvents()
	}
}

func (m *Manager) subscribeEvents(ctx context.Context) {
	for {
		err := m.client.SubscribeEvents(ctx, func(rawJSON string) {
			if m.eventHandler != nil {
				m.eventHandler("", rawJSON)
			}
		})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("event subscription failed: %v, retrying...", err)
		}
		time.Sleep(2 * time.Second)
	}
}

// Register creates a new instance record for the given working directory and
// adds it to the in-memory map with the shared client.
func (m *Manager) Register(ctx context.Context, workingDir, org, repo string) (*Instance, error) {
	id := uuid.New().String()

	if _, err := m.queries.CreateInstance(ctx, sqlc.CreateInstanceParams{
		ID:               id,
		WorkingDirectory: workingDir,
		Org:              org,
		Repo:             repo,
	}); err != nil {
		return nil, fmt.Errorf("create instance record: %w", err)
	}

	inst := &Instance{
		ID:               id,
		WorkingDirectory: workingDir,
		Org:              org,
		Repo:             repo,
		Status:           "running",
		Client:           m.client,
	}

	m.mu.Lock()
	m.instances[id] = inst
	m.mu.Unlock()

	log.Printf("registered instance %s for %s (org: %s, repo: %s)", id[:8], workingDir, org, repo)

	if m.instanceHook != nil {
		m.instanceHook("register", inst)
	}

	return inst, nil
}

// LoadExisting loads all instances from the database into the in-memory map.
// Called on startup so that instances survive flock restarts.
func (m *Manager) LoadExisting(ctx context.Context) error {
	dbInstances, err := m.queries.ListInstances(ctx)
	if err != nil {
		return fmt.Errorf("list instances: %w", err)
	}

	m.mu.Lock()
	for _, dbi := range dbInstances {
		m.instances[dbi.ID] = &Instance{
			ID:               dbi.ID,
			WorkingDirectory: dbi.WorkingDirectory,
			Org:              dbi.Org,
			Repo:             dbi.Repo,
			Status:           "running",
			Client:           m.client,
		}
		// Ensure DB status is "running" (may have been stale from a previous crash)
		m.queries.UpdateInstanceStatus(ctx, sqlc.UpdateInstanceStatusParams{
			Status: "running",
			ID:     dbi.ID,
		})
	}
	m.mu.Unlock()

	if n := len(dbInstances); n > 0 {
		log.Printf("loaded %d existing instance(s) from database", n)
	}
	return nil
}

// Stop removes an instance from the in-memory map and database.
func (m *Manager) Stop(ctx context.Context, id string) error {
	m.mu.Lock()
	_, ok := m.instances[id]
	if ok {
		delete(m.instances, id)
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("instance %s not found", id)
	}

	if m.instanceHook != nil {
		m.instanceHook("stop", &Instance{ID: id})
	}

	m.queries.DeleteSessionsByInstance(ctx, id)
	m.queries.DeleteInstance(ctx, id)
	return nil
}

// Get returns an instance by ID.
func (m *Manager) Get(id string) (*Instance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inst, ok := m.instances[id]
	return inst, ok
}

// List returns all tracked instances.
func (m *Manager) List() []*Instance {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Instance, 0, len(m.instances))
	for _, inst := range m.instances {
		result = append(result, inst)
	}
	return result
}
