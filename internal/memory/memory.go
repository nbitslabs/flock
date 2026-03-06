package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	flockDir     = ".flock"
	memoryDir    = ".flock/memory"
	progressDir  = ".flock/memory/progress"
	heartbeatFile = "HEARTBEAT.md"
	memoryFile   = "MEMORY.md"
	newTasksFile = "new_tasks.json"
	restartFile  = "restart_tasks.json"
)

// NewTaskDecision represents a new task from the orchestrator's decision file.
type NewTaskDecision struct {
	IssueNumber int    `json:"issue_number"`
	IssueURL    string `json:"issue_url"`
	Title       string `json:"title"`
	BranchName  string `json:"branch_name"`
}

// RestartTaskDecision represents a task restart request from the orchestrator.
type RestartTaskDecision struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason"`
}

// EnsureLayout creates the .flock directory structure and default files if they
// don't already exist. workingDir is the instance's working directory.
func EnsureLayout(workingDir string) error {
	dirs := []string{
		filepath.Join(workingDir, flockDir),
		filepath.Join(workingDir, memoryDir),
		filepath.Join(workingDir, progressDir),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	// Write default HEARTBEAT.md if missing
	hbPath := filepath.Join(workingDir, flockDir, heartbeatFile)
	if _, err := os.Stat(hbPath); os.IsNotExist(err) {
		if err := os.WriteFile(hbPath, []byte(defaultHeartbeat()), 0o644); err != nil {
			return fmt.Errorf("write heartbeat: %w", err)
		}
	}

	// Write default MEMORY.md if missing
	memPath := filepath.Join(workingDir, memoryDir, memoryFile)
	if _, err := os.Stat(memPath); os.IsNotExist(err) {
		if err := os.WriteFile(memPath, []byte(defaultMemory()), 0o644); err != nil {
			return fmt.Errorf("write memory: %w", err)
		}
	}

	return nil
}

// ReadHeartbeat returns the contents of .flock/HEARTBEAT.md.
func ReadHeartbeat(workingDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(workingDir, flockDir, heartbeatFile))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteHeartbeat writes content to .flock/HEARTBEAT.md.
func WriteHeartbeat(workingDir, content string) error {
	return os.WriteFile(filepath.Join(workingDir, flockDir, heartbeatFile), []byte(content), 0o644)
}

// ReadInstanceMemory returns the contents of .flock/memory/MEMORY.md.
func ReadInstanceMemory(workingDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(workingDir, memoryDir, memoryFile))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ReadGlobalMemory returns the global memory file from the data directory.
func ReadGlobalMemory(dataDir string) (string, error) {
	path := filepath.Join(dataDir, "memory", memoryFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// WriteGlobalMemory writes the global memory file in the data directory.
func WriteGlobalMemory(dataDir, content string) error {
	dir := filepath.Join(dataDir, "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, memoryFile), []byte(content), 0o644)
}

// ReadNewTasks reads and parses .flock/memory/new_tasks.json.
func ReadNewTasks(workingDir string) ([]NewTaskDecision, error) {
	path := filepath.Join(workingDir, memoryDir, newTasksFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var tasks []NewTaskDecision
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("parse new_tasks.json: %w", err)
	}
	return tasks, nil
}

// ReadRestartTasks reads and parses .flock/memory/restart_tasks.json.
func ReadRestartTasks(workingDir string) ([]RestartTaskDecision, error) {
	path := filepath.Join(workingDir, memoryDir, restartFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var tasks []RestartTaskDecision
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("parse restart_tasks.json: %w", err)
	}
	return tasks, nil
}

// ClearDecisionFiles removes the decision files after processing.
func ClearDecisionFiles(workingDir string) {
	os.Remove(filepath.Join(workingDir, memoryDir, newTasksFile))
	os.Remove(filepath.Join(workingDir, memoryDir, restartFile))
}
