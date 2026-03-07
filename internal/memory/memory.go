package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	flockDir      = ".flock"
	memoryDir     = ".flock/memory"
	progressDir   = ".flock/memory/progress"
	heartbeatFile = "HEARTBEAT.md"
	memoryFile    = "MEMORY.md"
	newTasksFile  = "new_tasks.json"
	restartFile   = "restart_tasks.json"
	completedFile = "completed_tasks.json"
)

// ResolveStateDir returns the state directory path.
// If the given path already ends with ".flock", it is returned as-is.
// Otherwise, ".flock" is appended to the path.
func ResolveStateDir(dataDir string) string {
	if strings.HasSuffix(dataDir, flockDir) {
		return dataDir
	}
	return filepath.Join(dataDir, flockDir)
}

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

// CompletedTaskDecision represents a task that the orchestrator has identified as completed.
type CompletedTaskDecision struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason"`
}

// EnsureLayout creates the .flock directory structure and default files if they
// don't already exist. workingDir is the instance's working directory.
// It detects if workingDir already ends with ".flock" to avoid nesting.
func EnsureLayout(workingDir string) error {
	stateDir := ResolveStateDir(workingDir)
	dirs := []string{
		stateDir,
		filepath.Join(stateDir, "memory"),
		filepath.Join(stateDir, "memory", "progress"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	// Write default HEARTBEAT.md if missing
	hbPath := filepath.Join(stateDir, heartbeatFile)
	if _, err := os.Stat(hbPath); os.IsNotExist(err) {
		if err := os.WriteFile(hbPath, []byte(defaultHeartbeat()), 0o644); err != nil {
			return fmt.Errorf("write heartbeat: %w", err)
		}
	}

	// Write default MEMORY.md if missing
	memPath := filepath.Join(stateDir, "memory", memoryFile)
	if _, err := os.Stat(memPath); os.IsNotExist(err) {
		if err := os.WriteFile(memPath, []byte(defaultMemory()), 0o644); err != nil {
			return fmt.Errorf("write memory: %w", err)
		}
	}

	return nil
}

// ReadHeartbeat returns the contents of .flock/HEARTBEAT.md.
func ReadHeartbeat(workingDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(ResolveStateDir(workingDir), heartbeatFile))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteHeartbeat writes content to .flock/HEARTBEAT.md.
func WriteHeartbeat(workingDir, content string) error {
	return os.WriteFile(filepath.Join(ResolveStateDir(workingDir), heartbeatFile), []byte(content), 0o644)
}

// ReadInstanceMemory returns the contents of .flock/memory/MEMORY.md.
func ReadInstanceMemory(workingDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(ResolveStateDir(workingDir), "memory", memoryFile))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ReadGlobalMemory returns the global memory file from the data directory.
func ReadGlobalMemory(dataDir string) (string, error) {
	path := filepath.Join(ResolveStateDir(dataDir), "memory", memoryFile)
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
	dir := filepath.Join(ResolveStateDir(dataDir), "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, memoryFile), []byte(content), 0o644)
}

// ReadNewTasks reads and parses .flock/memory/new_tasks.json.
func ReadNewTasks(workingDir string) ([]NewTaskDecision, error) {
	path := filepath.Join(ResolveStateDir(workingDir), "memory", newTasksFile)
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
	path := filepath.Join(ResolveStateDir(workingDir), "memory", restartFile)
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

// ReadCompletedTasks reads and parses .flock/memory/completed_tasks.json.
func ReadCompletedTasks(workingDir string) ([]CompletedTaskDecision, error) {
	path := filepath.Join(ResolveStateDir(workingDir), "memory", completedFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var tasks []CompletedTaskDecision
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("parse completed_tasks.json: %w", err)
	}
	return tasks, nil
}

// ClearDecisionFiles removes the decision files after processing.
func ClearDecisionFiles(workingDir string) {
	os.Remove(filepath.Join(ResolveStateDir(workingDir), "memory", newTasksFile))
	os.Remove(filepath.Join(ResolveStateDir(workingDir), "memory", restartFile))
	os.Remove(filepath.Join(ResolveStateDir(workingDir), "memory", completedFile))
}

// TemplateHash returns the SHA256 hash of the embedded heartbeat template.
func TemplateHash() string {
	hash := sha256.Sum256([]byte(defaultHeartbeat()))
	return hex.EncodeToString(hash[:])
}

// HeartbeatUpgradePrompt returns a prompt for OpenCode to merge the existing
// heartbeat with the new template.
func HeartbeatUpgradePrompt(workingDir string) (string, error) {
	existingHeartbeat, err := ReadHeartbeat(workingDir)
	if err != nil {
		return "", fmt.Errorf("read existing heartbeat: %w", err)
	}

	newHeartbeat := defaultHeartbeat()

	prompt := fmt.Sprintf(`The heartbeat template has been updated. Your task is to merge the existing heartbeat file with the new template, preserving any custom modifications while incorporating new changes.

## Existing Heartbeat (preserve custom modifications):
%s

## New Heartbeat Template (incorporate new changes):
%s

## Instructions:
1. Compare the existing heartbeat with the new template
2. Identify any custom modifications made by the user in the existing file
3. Merge them by:
   - Incorporating all new sections/instructions from the template
   - Preserving any user customizations that add value
   - Removing any instructions that are no longer relevant
4. Write the merged result to: %s

Only write the merged content to the file. Do not add any other files.`, existingHeartbeat, newHeartbeat, filepath.Join(ResolveStateDir(workingDir), heartbeatFile))

	return prompt, nil
}
