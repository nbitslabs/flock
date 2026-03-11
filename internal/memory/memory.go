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
	heartbeatFile = "HEARTBEAT.md"
	memoryFile    = "MEMORY.md"
	newTasksFile  = "new_tasks.json"
	restartFile   = "restart_tasks.json"
	completedFile = "completed_tasks.json"
)

// ResolveStateDir returns the state directory path.
func ResolveStateDir(dataDir string) string {
	if strings.HasSuffix(dataDir, flockDir) {
		return dataDir
	}
	return filepath.Join(dataDir, flockDir)
}

// --- New repo-based paths ---

// RepoStatePath returns the repo-specific state directory.
// e.g. {dataDir}/.flock/state/github.com/{org}/{repo}
func RepoStatePath(dataDir, org, repo string) string {
	return filepath.Join(ResolveStateDir(dataDir), "state", "github.com", org, repo)
}

// RepoProgressPath returns the repo-specific progress directory.
func RepoProgressPath(dataDir, org, repo string) string {
	return filepath.Join(RepoStatePath(dataDir, org, repo), "progress")
}

// RepoDecisionsPath returns the repo-specific decisions directory.
func RepoDecisionsPath(dataDir, org, repo string) string {
	return filepath.Join(RepoStatePath(dataDir, org, repo), "decisions")
}

// RepoWorktreePath returns the worktree path for a branch within a repo state.
// e.g. {dataDir}/.flock/state/github.com/{org}/{repo}/worktrees/{branchName}
func RepoWorktreePath(dataDir, org, repo, branchName string) string {
	return filepath.Join(RepoStatePath(dataDir, org, repo), "worktrees", branchName)
}

// ReflectionPath returns the path to the reflection directory.
func ReflectionPath(dataDir string) string {
	return filepath.Join(ResolveStateDir(dataDir), "memory", "reflection")
}

// --- Legacy paths (deprecated, kept for migration) ---

// InstanceMemoryPath returns the path to instance-specific memory directory.
// Deprecated: Use RepoStatePath instead.
func InstanceMemoryPath(dataDir, instanceID string) string {
	return filepath.Join(ResolveStateDir(dataDir), "memory", "instances", instanceID)
}

// InstanceProgressPath returns the path to instance-specific progress directory.
// Deprecated: Use RepoProgressPath instead.
func InstanceProgressPath(dataDir, instanceID string) string {
	return filepath.Join(InstanceMemoryPath(dataDir, instanceID), "progress")
}

// InstanceWorktreePath returns the global worktree path for an instance/branch.
// Deprecated: Use RepoWorktreePath instead.
func InstanceWorktreePath(dataDir, instanceID, branchName string) string {
	return filepath.Join(ResolveStateDir(dataDir), "worktrees", instanceID, branchName)
}

// GlobalWorktreesPath returns the global worktrees directory.
// Deprecated: Use RepoWorktreePath instead.
func GlobalWorktreesPath(dataDir string) string {
	return filepath.Join(ResolveStateDir(dataDir), "worktrees")
}

// --- Decision types ---

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

// --- Layout functions ---

// EnsureLayout creates the global .flock directory structure.
func EnsureLayout(dataDir string) error {
	stateDir := ResolveStateDir(dataDir)
	dirs := []string{
		stateDir,
		filepath.Join(stateDir, "memory"),
		filepath.Join(stateDir, "state"),
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

// EnsureRepoLayout creates the repo-specific directory structure and default files.
func EnsureRepoLayout(dataDir, org, repo string) error {
	repoDir := RepoStatePath(dataDir, org, repo)
	dirs := []string{
		repoDir,
		filepath.Join(repoDir, "progress"),
		filepath.Join(repoDir, "decisions"),
		filepath.Join(repoDir, "worktrees"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	// Write default HEARTBEAT.md if missing
	hbPath := filepath.Join(repoDir, heartbeatFile)
	if _, err := os.Stat(hbPath); os.IsNotExist(err) {
		if err := os.WriteFile(hbPath, []byte(defaultHeartbeat()), 0o644); err != nil {
			return fmt.Errorf("write heartbeat: %w", err)
		}
	}

	// Write default MEMORY.md if missing
	memPath := filepath.Join(repoDir, memoryFile)
	if _, err := os.Stat(memPath); os.IsNotExist(err) {
		if err := os.WriteFile(memPath, []byte(defaultMemory()), 0o644); err != nil {
			return fmt.Errorf("write memory: %w", err)
		}
	}

	return nil
}

// EnsureInstanceLayout creates the instance-specific directory structure.
// Deprecated: Use EnsureRepoLayout instead.
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

	hbPath := filepath.Join(instanceMemDir, heartbeatFile)
	if _, err := os.Stat(hbPath); os.IsNotExist(err) {
		if err := os.WriteFile(hbPath, []byte(defaultHeartbeat()), 0o644); err != nil {
			return fmt.Errorf("write heartbeat: %w", err)
		}
	}

	memPath := filepath.Join(instanceMemDir, memoryFile)
	if _, err := os.Stat(memPath); os.IsNotExist(err) {
		if err := os.WriteFile(memPath, []byte(defaultMemory()), 0o644); err != nil {
			return fmt.Errorf("write memory: %w", err)
		}
	}

	return nil
}

// --- Repo-based read/write functions ---

// ReadRepoHeartbeat returns the HEARTBEAT.md from repo state.
func ReadRepoHeartbeat(dataDir, org, repo string) (string, error) {
	path := filepath.Join(RepoStatePath(dataDir, org, repo), heartbeatFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteRepoHeartbeat writes HEARTBEAT.md to repo state.
func WriteRepoHeartbeat(dataDir, org, repo, content string) error {
	path := filepath.Join(RepoStatePath(dataDir, org, repo), heartbeatFile)
	return os.WriteFile(path, []byte(content), 0o644)
}

// ReadRepoMemory returns the MEMORY.md from repo state.
func ReadRepoMemory(dataDir, org, repo string) (string, error) {
	path := filepath.Join(RepoStatePath(dataDir, org, repo), memoryFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteRepoMemory writes MEMORY.md to repo state.
func WriteRepoMemory(dataDir, org, repo, content string) error {
	path := filepath.Join(RepoStatePath(dataDir, org, repo), memoryFile)
	return os.WriteFile(path, []byte(content), 0o644)
}

// ReadRepoNewTasks reads and parses new_tasks.json from repo decisions.
func ReadRepoNewTasks(dataDir, org, repo string) ([]NewTaskDecision, error) {
	path := filepath.Join(RepoDecisionsPath(dataDir, org, repo), newTasksFile)
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

// ReadRepoRestartTasks reads and parses restart_tasks.json from repo decisions.
func ReadRepoRestartTasks(dataDir, org, repo string) ([]RestartTaskDecision, error) {
	path := filepath.Join(RepoDecisionsPath(dataDir, org, repo), restartFile)
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

// ReadRepoCompletedTasks reads and parses completed_tasks.json from repo decisions.
func ReadRepoCompletedTasks(dataDir, org, repo string) ([]CompletedTaskDecision, error) {
	path := filepath.Join(RepoDecisionsPath(dataDir, org, repo), completedFile)
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

// ClearRepoDecisionFiles removes the decision files after processing.
func ClearRepoDecisionFiles(dataDir, org, repo string) {
	decisionsDir := RepoDecisionsPath(dataDir, org, repo)
	os.Remove(filepath.Join(decisionsDir, newTasksFile))
	os.Remove(filepath.Join(decisionsDir, restartFile))
	os.Remove(filepath.Join(decisionsDir, completedFile))
}

// --- Legacy instance-based read/write (deprecated) ---

// ReadHeartbeat returns the contents of instance-specific HEARTBEAT.md.
// Deprecated: Use ReadRepoHeartbeat instead.
func ReadHeartbeat(dataDir, instanceID string) (string, error) {
	hbPath := filepath.Join(InstanceMemoryPath(dataDir, instanceID), heartbeatFile)
	data, err := os.ReadFile(hbPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteHeartbeat writes content to instance-specific HEARTBEAT.md.
// Deprecated: Use WriteRepoHeartbeat instead.
func WriteHeartbeat(dataDir, instanceID, content string) error {
	hbPath := filepath.Join(InstanceMemoryPath(dataDir, instanceID), heartbeatFile)
	return os.WriteFile(hbPath, []byte(content), 0o644)
}

// ReadInstanceMemory returns the contents of instance-specific MEMORY.md.
// Deprecated: Use ReadRepoMemory instead.
func ReadInstanceMemory(dataDir, instanceID string) (string, error) {
	memPath := filepath.Join(InstanceMemoryPath(dataDir, instanceID), memoryFile)
	data, err := os.ReadFile(memPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteInstanceMemory writes content to instance-specific MEMORY.md.
// Deprecated: Use WriteRepoMemory instead.
func WriteInstanceMemory(dataDir, instanceID, content string) error {
	memPath := filepath.Join(InstanceMemoryPath(dataDir, instanceID), memoryFile)
	return os.WriteFile(memPath, []byte(content), 0o644)
}

// ReadNewTasks reads and parses the instance's new_tasks.json.
// Deprecated: Use ReadRepoNewTasks instead.
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
// Deprecated: Use ReadRepoRestartTasks instead.
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
// Deprecated: Use ReadRepoCompletedTasks instead.
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
// Deprecated: Use ClearRepoDecisionFiles instead.
func ClearDecisionFiles(dataDir, instanceID string) {
	os.Remove(filepath.Join(InstanceMemoryPath(dataDir, instanceID), newTasksFile))
	os.Remove(filepath.Join(InstanceMemoryPath(dataDir, instanceID), restartFile))
	os.Remove(filepath.Join(InstanceMemoryPath(dataDir, instanceID), completedFile))
}

// --- Global memory ---

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

// --- Template hash & upgrade ---

// TemplateHash returns the SHA256 hash of the embedded heartbeat template.
func TemplateHash() string {
	hash := sha256.Sum256([]byte(defaultHeartbeat()))
	return hex.EncodeToString(hash[:])
}

// HeartbeatUpgradePrompt returns a prompt for OpenCode to merge the existing
// heartbeat with the new template.
func HeartbeatUpgradePrompt(dataDir, org, repo string) (string, error) {
	existingHeartbeat, err := ReadRepoHeartbeat(dataDir, org, repo)
	if err != nil {
		return "", fmt.Errorf("read existing heartbeat: %w", err)
	}

	newHeartbeat := defaultHeartbeat()

	hbPath := filepath.Join(RepoStatePath(dataDir, org, repo), heartbeatFile)
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

// --- Migration ---

// MigrateInstanceToRepo migrates an old UUID-based instance directory to the new
// repo-based layout. It copies contents from .flock/memory/instances/{instanceID}
// to .flock/state/github.com/{org}/{repo} and worktrees from
// .flock/worktrees/{instanceID}/ to the repo state.
func MigrateInstanceToRepo(dataDir, instanceID, org, repo string) error {
	if org == "" || repo == "" {
		return fmt.Errorf("org and repo must be non-empty for migration")
	}

	// Ensure the new layout exists
	if err := EnsureRepoLayout(dataDir, org, repo); err != nil {
		return fmt.Errorf("ensure repo layout: %w", err)
	}

	repoDir := RepoStatePath(dataDir, org, repo)
	oldInstanceDir := InstanceMemoryPath(dataDir, instanceID)

	// Migrate memory files (HEARTBEAT.md, MEMORY.md)
	for _, f := range []string{heartbeatFile, memoryFile} {
		oldPath := filepath.Join(oldInstanceDir, f)
		newPath := filepath.Join(repoDir, f)
		if data, err := os.ReadFile(oldPath); err == nil {
			// Only overwrite if the new file is still the default
			existing, _ := os.ReadFile(newPath)
			if string(existing) == defaultHeartbeat() || string(existing) == defaultMemory() || len(existing) == 0 {
				os.WriteFile(newPath, data, 0o644)
			}
		}
	}

	// Migrate progress files
	oldProgress := filepath.Join(oldInstanceDir, "progress")
	newProgress := filepath.Join(repoDir, "progress")
	migrateDir(oldProgress, newProgress)

	// Migrate decision files to decisions/ subdirectory
	newDecisions := filepath.Join(repoDir, "decisions")
	for _, f := range []string{newTasksFile, restartFile, completedFile} {
		oldPath := filepath.Join(oldInstanceDir, f)
		newPath := filepath.Join(newDecisions, f)
		if data, err := os.ReadFile(oldPath); err == nil {
			os.WriteFile(newPath, data, 0o644)
			os.Remove(oldPath)
		}
	}

	// Migrate worktrees
	stateDir := ResolveStateDir(dataDir)
	oldWorktreeDir := filepath.Join(stateDir, "worktrees", instanceID)
	newWorktreeDir := filepath.Join(repoDir, "worktrees")
	migrateDir(oldWorktreeDir, newWorktreeDir)

	return nil
}

// migrateDir copies files from src to dst, skipping files that already exist in dst.
func migrateDir(src, dst string) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return
	}
	os.MkdirAll(dst, 0o755)
	for _, entry := range entries {
		oldPath := filepath.Join(src, entry.Name())
		newPath := filepath.Join(dst, entry.Name())
		if _, err := os.Stat(newPath); err == nil {
			continue // already exists
		}
		if entry.IsDir() {
			// For worktrees, we can't just copy — they're git worktrees.
			// Just note the location; actual git worktree re-add happens elsewhere.
			continue
		}
		if data, err := os.ReadFile(oldPath); err == nil {
			os.WriteFile(newPath, data, 0o644)
		}
	}
}
