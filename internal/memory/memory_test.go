package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveStateDir(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "regular directory",
			input:    "/Users/test/project",
			expected: "/Users/test/project/.flock",
		},
		{
			name:     "directory already ending with .flock",
			input:    "/Users/test/project/.flock",
			expected: "/Users/test/project/.flock",
		},
		{
			name:     "current directory",
			input:    ".",
			expected: ".flock",
		},
		{
			name:     "current directory with .flock",
			input:    ".flock",
			expected: ".flock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveStateDir(tt.input)
			if result != tt.expected {
				t.Errorf("ResolveStateDir(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestEnsureLayoutWithFlockSuffix(t *testing.T) {
	tmpDir := t.TempDir()

	flockDir := filepath.Join(tmpDir, ".flock")
	if err := os.MkdirAll(flockDir, 0o755); err != nil {
		t.Fatalf("failed to create .flock dir: %v", err)
	}

	if err := EnsureLayout(tmpDir); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	expectedMemoryDir := filepath.Join(flockDir, "memory")
	if _, err := os.Stat(expectedMemoryDir); os.IsNotExist(err) {
		t.Errorf("expected memory dir at %s, but it doesn't exist", expectedMemoryDir)
	}

	expectedStateDir := filepath.Join(flockDir, "state")
	if _, err := os.Stat(expectedStateDir); os.IsNotExist(err) {
		t.Errorf("expected state dir at %s, but it doesn't exist", expectedStateDir)
	}

	wrongMemoryDir := filepath.Join(flockDir, ".flock", "memory")
	if _, err := os.Stat(wrongMemoryDir); !os.IsNotExist(err) {
		t.Errorf("found nested .flock directory at %s, should not exist", wrongMemoryDir)
	}
}

func TestEnsureLayoutWithoutFlockSuffix(t *testing.T) {
	tmpDir := t.TempDir()

	if err := EnsureLayout(tmpDir); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	expectedMemoryDir := filepath.Join(tmpDir, ".flock", "memory")
	if _, err := os.Stat(expectedMemoryDir); os.IsNotExist(err) {
		t.Errorf("expected memory dir at %s, but it doesn't exist", expectedMemoryDir)
	}
}

func TestRepoStatePath(t *testing.T) {
	result := RepoStatePath("/data", "myorg", "myrepo")
	expected := "/data/.flock/state/github.com/myorg/myrepo"
	if result != expected {
		t.Errorf("RepoStatePath = %q, want %q", result, expected)
	}
}

func TestRepoWorktreePath(t *testing.T) {
	result := RepoWorktreePath("/data", "myorg", "myrepo", "fix/issue-42")
	expected := "/data/.flock/state/github.com/myorg/myrepo/worktrees/fix/issue-42"
	if result != expected {
		t.Errorf("RepoWorktreePath = %q, want %q", result, expected)
	}
}

func TestEnsureRepoLayout(t *testing.T) {
	tmpDir := t.TempDir()

	if err := EnsureRepoLayout(tmpDir, "testorg", "testrepo"); err != nil {
		t.Fatalf("EnsureRepoLayout failed: %v", err)
	}

	repoDir := filepath.Join(tmpDir, ".flock", "state", "github.com", "testorg", "testrepo")

	// Check directories
	for _, sub := range []string{"progress", "decisions", "worktrees"} {
		dir := filepath.Join(repoDir, sub)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("expected dir %s to exist", dir)
		}
	}

	// Check default files
	for _, f := range []string{"HEARTBEAT.md", "MEMORY.md"} {
		path := filepath.Join(repoDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %s to exist", path)
		}
	}
}

func TestRepoDecisionReadWrite(t *testing.T) {
	tmpDir := t.TempDir()

	if err := EnsureRepoLayout(tmpDir, "org", "repo"); err != nil {
		t.Fatalf("EnsureRepoLayout failed: %v", err)
	}

	// Write a new_tasks.json
	decisionsDir := RepoDecisionsPath(tmpDir, "org", "repo")
	os.WriteFile(filepath.Join(decisionsDir, "new_tasks.json"),
		[]byte(`[{"issue_number":1,"issue_url":"http://x","title":"test","branch_name":"fix/test"}]`), 0o644)

	tasks, err := ReadRepoNewTasks(tmpDir, "org", "repo")
	if err != nil {
		t.Fatalf("ReadRepoNewTasks failed: %v", err)
	}
	if len(tasks) != 1 || tasks[0].IssueNumber != 1 {
		t.Errorf("unexpected tasks: %+v", tasks)
	}

	ClearRepoDecisionFiles(tmpDir, "org", "repo")

	tasks, err = ReadRepoNewTasks(tmpDir, "org", "repo")
	if err != nil {
		t.Fatalf("ReadRepoNewTasks after clear failed: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected empty tasks after clear, got %d", len(tasks))
	}
}
