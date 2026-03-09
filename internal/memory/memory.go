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
	flockDir        = ".flock"
	memoryDir       = ".flock/memory"
	instancesDir    = ".flock/memory/instances"
	progressDir     = ".flock/memory/progress"
	heartbeatFile   = "HEARTBEAT.md"
	memoryFile      = "MEMORY.md"
	worktreesDir    = ".flock/worktrees"
	newTasksFile    = "new_tasks.json"
	restartFile     = "restart_tasks.json"
	completedFile   = "completed_tasks.json"
	reflectionDir   = ".flock/memory/reflection"
)

// ResolveStateDir returns the state directory path.
func ResolveStateDir(dataDir string) string {
	if strings.HasSuffix(dataDir, flockDir) {
		return dataDir
	}
	return filepath.Join(dataDir, flockDir)
}

// InstanceMemoryPath returns the path to instance-specific memory directory.
func InstanceMemoryPath(dataDir, instanceID string) string {
	return filepath.Join(ResolveStateDir(dataDir), "memory", "instances", instanceID)
}

// InstanceProgressPath returns the path to instance-specific progress directory.
func InstanceProgressPath(dataDir, instanceID string) string {
	return filepath.Join(InstanceMemoryPath(dataDir, instanceID), "progress")
}

// InstanceWorktreePath returns the global worktree path for an instance/branch.
func InstanceWorktreePath(dataDir, instanceID, branchName string) string {
	return filepath.Join(ResolveStateDir(dataDir), "worktrees", instanceID, branchName)
}

// GlobalWorktreesPath returns the global worktrees directory.
func GlobalWorktreesPath(dataDir string) string {
	return filepath.Join(ResolveStateDir(dataDir), "worktrees")
}

// ReflectionPath returns the path to the reflection directory.
func ReflectionPath(dataDir string) string {
	return filepath.Join(ResolveStateDir(dataDir), "memory", "reflection")
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

// EnsureLayout creates the .flock directory structure and default files in the data directory.
// It creates the global flock structure and instance-specific directories.
func EnsureLayout(dataDir string) error {
	stateDir := ResolveStateDir(dataDir)
	dirs := []string{
		stateDir,
		filepath.Join(stateDir, "memory"),
		filepath.Join(stateDir, "memory", "instances"),
		filepath.Join(stateDir, "worktrees"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	// Write default global MEMORY.md if missing
	memPath := filepath.Join(stateDir, "memory", memoryFile)
	if _, err := os.Stat(memPath); os.IsNotExist(err) {
		if err := os.WriteFile(memPath, []byte(defaultMemory()), 0o644); err != nil {
			return fmt.Errorf("write memory: %w", err)
		}
	}

	return nil
}

// EnsureInstanceLayout creates the instance-specific directory structure and default files.
// instanceID is used to create instance-specific directories within dataDir.
func EnsureInstanceLayout(dataDir, instanceID string) error {
	stateDir := ResolveStateDir(dataDir)
	instanceMemDir := filepath.Join(stateDir, "memory", "instances", instanceID)
	dirs := []string{
		instanceMemDir,
		filepath.Join(instanceMemDir, "progress"),
		filepath.Join(stateDir, "worktrees", instanceID),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	// Write default HEARTBEAT.md if missing
	hbPath := filepath.Join(instanceMemDir, heartbeatFile)
	if _, err := os.Stat(hbPath); os.IsNotExist(err) {
		if err := os.WriteFile(hbPath, []byte(defaultHeartbeat()), 0o644); err != nil {
			return fmt.Errorf("write heartbeat: %w", err)
		}
	}

	// Write default MEMORY.md if missing
	memPath := filepath.Join(instanceMemDir, memoryFile)
	if _, err := os.Stat(memPath); os.IsNotExist(err) {
		if err := os.WriteFile(memPath, []byte(defaultMemory()), 0o644); err != nil {
			return fmt.Errorf("write memory: %w", err)
		}
	}

	return nil
}

// ReadHeartbeat returns the contents of instance-specific HEARTBEAT.md.
func ReadHeartbeat(dataDir, instanceID string) (string, error) {
	hbPath := filepath.Join(InstanceMemoryPath(dataDir, instanceID), heartbeatFile)
	data, err := os.ReadFile(hbPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteHeartbeat writes content to instance-specific HEARTBEAT.md.
func WriteHeartbeat(dataDir, instanceID, content string) error {
	hbPath := filepath.Join(InstanceMemoryPath(dataDir, instanceID), heartbeatFile)
	return os.WriteFile(hbPath, []byte(content), 0o644)
}

// ReadInstanceMemory returns the contents of instance-specific MEMORY.md.
func ReadInstanceMemory(dataDir, instanceID string) (string, error) {
	memPath := filepath.Join(InstanceMemoryPath(dataDir, instanceID), memoryFile)
	data, err := os.ReadFile(memPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteInstanceMemory writes content to instance-specific MEMORY.md.
func WriteInstanceMemory(dataDir, instanceID, content string) error {
	memPath := filepath.Join(InstanceMemoryPath(dataDir, instanceID), memoryFile)
	return os.WriteFile(memPath, []byte(content), 0o644)
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

// ReadNewTasks reads and parses the instance's new_tasks.json.
func ReadNewTasks(dataDir, instanceID string) ([]NewTaskDecision, error) {
	path := filepath.Join(InstanceMemoryPath(dataDir, instanceID), newTasksFile)
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

// ReadRestartTasks reads and parses the instance's restart_tasks.json.
func ReadRestartTasks(dataDir, instanceID string) ([]RestartTaskDecision, error) {
	path := filepath.Join(InstanceMemoryPath(dataDir, instanceID), restartFile)
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

// ReadCompletedTasks reads and parses the instance's completed_tasks.json.
func ReadCompletedTasks(dataDir, instanceID string) ([]CompletedTaskDecision, error) {
	path := filepath.Join(InstanceMemoryPath(dataDir, instanceID), completedFile)
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
func ClearDecisionFiles(dataDir, instanceID string) {
	os.Remove(filepath.Join(InstanceMemoryPath(dataDir, instanceID), newTasksFile))
	os.Remove(filepath.Join(InstanceMemoryPath(dataDir, instanceID), restartFile))
	os.Remove(filepath.Join(InstanceMemoryPath(dataDir, instanceID), completedFile))
}

// TemplateHash returns the SHA256 hash of the embedded heartbeat template.
func TemplateHash() string {
	hash := sha256.Sum256([]byte(defaultHeartbeat()))
	return hex.EncodeToString(hash[:])
}

// HeartbeatUpgradePrompt returns a prompt for OpenCode to merge the existing
// heartbeat with the new template.
func HeartbeatUpgradePrompt(dataDir, instanceID string) (string, error) {
	existingHeartbeat, err := ReadHeartbeat(dataDir, instanceID)
	if err != nil {
		return "", fmt.Errorf("read existing heartbeat: %w", err)
	}

	newHeartbeat := defaultHeartbeat()

	hbPath := filepath.Join(InstanceMemoryPath(dataDir, instanceID), heartbeatFile)
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

Only write the merged content to the file. Do not add any other files.`, existingHeartbeat, newHeartbeat, hbPath)

	return prompt, nil
}
