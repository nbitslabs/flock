package agent

import "testing"

func TestIsBlockedGitCommand(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		blocked bool
	}{
		{"empty args", nil, false},
		{"git status", []string{"status"}, false},
		{"git add", []string{"add", "."}, false},
		{"git commit", []string{"commit", "-m", "msg"}, false},
		{"git push", []string{"push", "-u", "origin", "main"}, false},
		{"git diff", []string{"diff"}, false},
		{"git log", []string{"log", "--oneline"}, false},
		{"git checkout blocked", []string{"checkout", "main"}, true},
		{"git switch blocked", []string{"switch", "main"}, true},
		{"git worktree add blocked", []string{"worktree", "add", "/tmp/wt"}, true},
		{"git worktree remove blocked", []string{"worktree", "remove", "/tmp/wt"}, true},
		{"git worktree move blocked", []string{"worktree", "move", "/a", "/b"}, true},
		{"git worktree prune blocked", []string{"worktree", "prune"}, true},
		{"git worktree list allowed", []string{"worktree", "list"}, false},
		{"git worktree repair allowed", []string{"worktree", "repair"}, false},
		{"git with flags then checkout", []string{"-C", "/path", "checkout", "main"}, true},
		{"git with flags then status", []string{"-C", "/path", "status"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocked, msg := IsBlockedGitCommand(tt.args)
			if blocked != tt.blocked {
				t.Errorf("IsBlockedGitCommand(%v) = (%v, %q), want blocked=%v", tt.args, blocked, msg, tt.blocked)
			}
			if blocked && msg == "" {
				t.Errorf("IsBlockedGitCommand(%v) returned blocked=true but empty message", tt.args)
			}
		})
	}
}

func TestFilterWorktreeListOutput(t *testing.T) {
	output := `/home/user/repo                abc1234 [main]
/home/user/.flock/state/github.com/org/repo/worktrees/fix/issue-1  def5678 [fix/issue-1]
/home/user/.flock/state/github.com/org/repo/worktrees/fix/issue-2  ghi9012 [fix/issue-2]
`
	assigned := "/home/user/.flock/state/github.com/org/repo/worktrees/fix/issue-1"

	result := FilterWorktreeListOutput(output, assigned)
	if !contains(result, assigned) {
		t.Errorf("expected output to contain assigned worktree path")
	}
	if contains(result, "fix/issue-2") {
		t.Errorf("expected output to NOT contain other worktree paths")
	}
	if contains(result, "[main]") {
		t.Errorf("expected output to NOT contain main repo line")
	}
}

func TestFilterWorktreeListOutput_NoMatch(t *testing.T) {
	output := `/home/user/repo  abc1234 [main]
`
	assigned := "/nonexistent/path"

	result := FilterWorktreeListOutput(output, assigned)
	if result != output {
		t.Errorf("expected unfiltered output when no match, got %q", result)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
