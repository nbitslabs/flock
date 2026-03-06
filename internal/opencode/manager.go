package opencode

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/nbitslabs/flock/internal/db/sqlc"
)

// Instance represents a running OpenCode ACP process.
type Instance struct {
	ID               string
	Pid              int
	Port             int
	WorkingDirectory string
	Status           string
	Client           *Client
	cmd              *exec.Cmd
	cancel           context.CancelFunc
}

// EventHandler is called when an SSE event arrives from an OpenCode instance.
// rawJSON is the full JSON object: {"type":"...", "properties":{...}}
type EventHandler func(instanceID, rawJSON string)

// Manager manages OpenCode ACP instance lifecycles.
type Manager struct {
	mu           sync.RWMutex
	instances    map[string]*Instance
	queries      *sqlc.Queries
	eventHandler EventHandler
	opencodePath string
}

func NewManager(queries *sqlc.Queries, handler EventHandler) *Manager {
	// Resolve opencode binary path at startup
	path, err := exec.LookPath("opencode")
	if err != nil {
		log.Printf("WARNING: opencode not found in PATH, using 'opencode': %v", err)
		path = "opencode"
	} else {
		log.Printf("opencode found at %s", path)
	}
	return &Manager{
		instances:    make(map[string]*Instance),
		queries:      queries,
		eventHandler: handler,
		opencodePath: path,
	}
}

// Matches: "opencode server listening on http://127.0.0.1:19876"
var portRegexp = regexp.MustCompile(`listening on http://(?:localhost|127\.0\.0\.1|0\.0\.0\.0):(\d+)`)

// Spawn starts a new OpenCode ACP instance for the given working directory.
func (m *Manager) Spawn(ctx context.Context, workingDir string) (*Instance, error) {
	id := uuid.New().String()

	// Create DB record
	_, err := m.queries.CreateInstance(ctx, sqlc.CreateInstanceParams{
		ID:               id,
		Pid:              0,
		Port:             0,
		WorkingDirectory: workingDir,
		Status:           "starting",
	})
	if err != nil {
		return nil, fmt.Errorf("create instance record: %w", err)
	}

	procCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(procCtx, m.opencodePath, "serve")
	cmd.Dir = workingDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		m.queries.UpdateInstanceStatus(ctx, sqlc.UpdateInstanceStatusParams{Status: "error", ID: id})
		return nil, fmt.Errorf("start opencode: %w", err)
	}

	inst := &Instance{
		ID:               id,
		Pid:              cmd.Process.Pid,
		WorkingDirectory: workingDir,
		Status:           "starting",
		cmd:              cmd,
		cancel:           cancel,
	}

	// Update the DB record with the actual PID
	m.queries.UpdateInstanceStatus(ctx, sqlc.UpdateInstanceStatusParams{Status: "starting", ID: id})

	m.mu.Lock()
	m.instances[id] = inst
	m.mu.Unlock()

	// Discover port from stderr/stdout
	go m.discoverPort(inst, stderr)
	go m.discoverPort(inst, stdout)

	// Wait for process to exit
	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		inst.Status = "stopped"
		m.mu.Unlock()
		m.queries.UpdateInstanceStatus(context.Background(), sqlc.UpdateInstanceStatusParams{Status: "stopped", ID: id})
		if err != nil {
			log.Printf("opencode instance %s exited: %v", id, err)
		}
	}()

	return inst, nil
}

func (m *Manager) discoverPort(inst *Instance, r interface{ Read([]byte) (int, error) }) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		log.Printf("[opencode:%s] %s", inst.ID[:8], line)
		matches := portRegexp.FindStringSubmatch(line)
		if matches != nil {
			port, err := strconv.Atoi(matches[1])
			if err != nil {
				continue
			}
			m.mu.Lock()
			if inst.Port == 0 {
				inst.Port = port
				inst.Status = "running"
				inst.Client = NewClient(fmt.Sprintf("http://localhost:%d", port))
			}
			m.mu.Unlock()

			m.queries.UpdateInstancePort(context.Background(), sqlc.UpdateInstancePortParams{Port: int64(port), ID: inst.ID})
			m.queries.UpdateInstanceStatus(context.Background(), sqlc.UpdateInstanceStatusParams{Status: "running", ID: inst.ID})

			log.Printf("opencode instance %s running on port %d", inst.ID[:8], port)

			// Start event subscription
			go m.subscribeEvents(inst)
		}
	}
}

func (m *Manager) subscribeEvents(inst *Instance) {
	m.mu.RLock()
	client := inst.Client
	m.mu.RUnlock()
	if client == nil {
		return
	}

	for {
		err := client.SubscribeEvents(context.Background(), func(rawJSON string) {
			if m.eventHandler != nil {
				m.eventHandler(inst.ID, rawJSON)
			}
		})
		if err != nil {
			log.Printf("event subscription for %s failed: %v, retrying...", inst.ID[:8], err)
		}
		// Check if instance still running
		m.mu.RLock()
		status := inst.Status
		m.mu.RUnlock()
		if status == "stopped" {
			return
		}
		time.Sleep(2 * time.Second)
	}
}

// killProcess kills the process for an instance and removes it from the in-memory map.
func (m *Manager) killProcess(id string) {
	m.mu.Lock()
	inst, ok := m.instances[id]
	if ok {
		delete(m.instances, id)
	}
	m.mu.Unlock()

	if !ok {
		return
	}

	if inst.cmd != nil && inst.cmd.Process != nil {
		_ = syscall.Kill(-inst.cmd.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			inst.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-inst.cmd.Process.Pid, syscall.SIGKILL)
		}
	}
	if inst.cancel != nil {
		inst.cancel()
	}
}

// Stop gracefully stops an OpenCode instance and removes it from the database.
func (m *Manager) Stop(ctx context.Context, id string) error {
	m.mu.RLock()
	_, ok := m.instances[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("instance %s not found", id)
	}

	m.killProcess(id)
	m.queries.UpdateInstanceStatus(ctx, sqlc.UpdateInstanceStatusParams{Status: "stopped", ID: id})
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

// StopAll stops all running instances for graceful shutdown.
// Preserves DB records so instances can be restored after restart.
func (m *Manager) StopAll(ctx context.Context) {
	m.mu.RLock()
	ids := make([]string, 0, len(m.instances))
	for id := range m.instances {
		ids = append(ids, id)
	}
	m.mu.RUnlock()

	for _, id := range ids {
		m.killProcess(id)
		m.queries.UpdateInstanceStatus(ctx, sqlc.UpdateInstanceStatusParams{Status: "stopped", ID: id})
	}
}

// RestoreInstance restores a stopped instance by spawning a new OpenCode process
// for the same working directory. The old DB record is cleaned up.
func (m *Manager) RestoreInstance(ctx context.Context, instID string) (*Instance, error) {
	dbInst, err := m.queries.GetInstance(ctx, instID)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}

	workDir := dbInst.WorkingDirectory

	// Clean up old record and its sessions
	m.queries.DeleteSessionsByInstance(ctx, instID)
	m.queries.DeleteInstance(ctx, instID)

	// Spawn a new process (creates new DB record with new ID)
	newInst, err := m.Spawn(ctx, workDir)
	if err != nil {
		return nil, fmt.Errorf("spawn instance: %w", err)
	}

	log.Printf("restored instance for %s (old: %s, new: %s)", workDir, instID[:8], newInst.ID[:8])
	return newInst, nil
}
