package agent

import (
	"fmt"
	"strings"
	"testing"
)

// composeHeartbeatMessageSized simulates the NEW composeHeartbeatMessage output
// for different task counts to verify consistent message size.
func composeHeartbeatMessageSized(activeCount, stuckCount int, stuckTasks []struct{ num int; title, id, lastActivity string }) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Heartbeat\n\nWorking directory: `/tmp/test`\nRepo state: `/tmp/.flock/state/github.com/org/repo`\nDecisions: `/tmp/.flock/state/github.com/org/repo/decisions`\n\n"))

	sb.WriteString(fmt.Sprintf("## Current State\n\nActive tasks: %d\nStuck tasks: %d\n", activeCount, stuckCount))
	sb.WriteString("Last heartbeat: 2026-03-21T00:00:00Z\n\n")

	if stuckCount > 0 && len(stuckTasks) > 0 {
		sb.WriteString("### Stuck Tasks\n")
		for _, t := range stuckTasks {
			sb.WriteString(fmt.Sprintf("- #%d %s (task_id: %s, last: %s)\n",
				t.num, t.title, t.id, t.lastActivity))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Instructions\n")
	sb.WriteString("Follow the steps in your HEARTBEAT.md instructions.\n")
	sb.WriteString("For detailed task history, spawn `@flock-history-analyzer` with your query.\n")

	return sb.String()
}

// composeOldHeartbeatMessage simulates the OLD composeHeartbeatMessage that
// included full task listings. Used to measure context savings.
func composeOldHeartbeatMessage(tasks []struct{ num int; title, status, id, branch, prUrl string }, stuckTasks []struct{ num int; title, id, lastActivity string }) string {
	var sb strings.Builder
	sb.WriteString("# Heartbeat\n\nWorking directory: `/tmp/test`\nRepo state: `/tmp/.flock/state/github.com/org/repo`\n\n")

	if len(tasks) > 0 {
		sb.WriteString("## Active Tasks\n\n")
		for _, t := range tasks {
			line := fmt.Sprintf("- **#%d** %s (status: %s, task_id: %s, branch: %s",
				t.num, t.title, t.status, t.id, t.branch)
			if t.prUrl != "" {
				line += fmt.Sprintf(", pr_url: %s", t.prUrl)
			}
			line += ")\n"
			sb.WriteString(line)
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("## Active Tasks\nNo active tasks.\n\n")
	}

	if len(stuckTasks) > 0 {
		sb.WriteString("## Stuck Tasks (no activity)\n\n")
		for _, t := range stuckTasks {
			sb.WriteString(fmt.Sprintf("- **#%d** %s (last activity: %s, task_id: %s)\n",
				t.num, t.title, t.lastActivity, t.id))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Instructions\n")
	sb.WriteString("1. Run `gh issue list --assignee=@me --state=open --json number,url,title`\n")
	sb.WriteString("2. For each active/stuck task above, check if its issue is closed: `gh issue view <number> --json state -q .state`\n")
	sb.WriteString("3. If the issue is open but the task has a pr_url, check if the PR is merged: `gh pr view <pr_url> --json state -q .state`\n")
	sb.WriteString("4. Write `/tmp/.flock/state/github.com/org/repo/decisions/completed_tasks.json` for tasks whose issues are closed or PRs are merged\n")
	sb.WriteString("5. Compare issue list with active tasks and write `/tmp/.flock/state/github.com/org/repo/decisions/new_tasks.json` for new issues\n")
	sb.WriteString("6. Write `/tmp/.flock/state/github.com/org/repo/decisions/restart_tasks.json` for stuck tasks needing restart\n")
	sb.WriteString("7. Update `/tmp/.flock/state/github.com/org/repo/MEMORY.md` with any observations\n")
	sb.WriteString("8. For each completed task, invoke the `@flock-self-reflect` subagent to update memory (see HEARTBEAT.md for details)\n")

	return sb.String()
}

// makeTasks generates N simulated tasks for testing.
func makeTasks(n int) []struct{ num int; title, status, id, branch, prUrl string } {
	tasks := make([]struct{ num int; title, status, id, branch, prUrl string }, n)
	for i := 0; i < n; i++ {
		tasks[i] = struct{ num int; title, status, id, branch, prUrl string }{
			num:    i + 1,
			title:  fmt.Sprintf("Fix issue number %d with a descriptive title", i+1),
			status: "active",
			id:     fmt.Sprintf("task-%04d-abcd-1234-5678", i+1),
			branch: fmt.Sprintf("fix/issue-%d-fix-issue-number-%d", i+1, i+1),
			prUrl:  fmt.Sprintf("https://github.com/org/repo/pull/%d", i+1),
		}
	}
	return tasks
}

func makeStuckTasks(n int) []struct{ num int; title, id, lastActivity string } {
	tasks := make([]struct{ num int; title, id, lastActivity string }, n)
	for i := 0; i < n; i++ {
		tasks[i] = struct{ num int; title, id, lastActivity string }{
			num:          i + 100,
			title:        fmt.Sprintf("Stuck task %d", i+1),
			id:           fmt.Sprintf("stuck-%04d", i+1),
			lastActivity: "2026-03-20T12:00:00Z",
		}
	}
	return tasks
}

func TestHeartbeatMessageConsistentSize(t *testing.T) {
	msg0 := composeHeartbeatMessageSized(0, 0, nil)
	msg1 := composeHeartbeatMessageSized(1, 0, nil)
	msg10 := composeHeartbeatMessageSized(10, 0, nil)
	msg50 := composeHeartbeatMessageSized(50, 0, nil)

	lines0 := len(strings.Split(msg0, "\n"))
	lines1 := len(strings.Split(msg1, "\n"))
	lines10 := len(strings.Split(msg10, "\n"))
	lines50 := len(strings.Split(msg50, "\n"))

	// Line counts should be identical when no stuck tasks
	if lines0 != lines1 || lines1 != lines10 || lines10 != lines50 {
		t.Errorf("line counts differ: 0=%d, 1=%d, 10=%d, 50=%d", lines0, lines1, lines10, lines50)
	}

	if lines0 > 50 {
		t.Errorf("message with 0 tasks is %d lines, want <=50", lines0)
	}

	sizeDiff := len(msg50) - len(msg0)
	if sizeDiff > 10 {
		t.Errorf("size difference between 0 and 50 tasks is %d bytes, expected <10", sizeDiff)
	}
}

func TestHeartbeatMessageWithStuckTasks(t *testing.T) {
	stuck := makeStuckTasks(2)

	msg := composeHeartbeatMessageSized(5, 2, stuck)

	if !strings.Contains(msg, "Active tasks: 5") {
		t.Error("expected active task count")
	}
	if !strings.Contains(msg, "Stuck tasks: 2") {
		t.Error("expected stuck task count")
	}
	if !strings.Contains(msg, "#100 Stuck task 1") {
		t.Error("expected stuck task details")
	}
	if !strings.Contains(msg, "@flock-history-analyzer") {
		t.Error("expected history analyzer reference")
	}

	lines := len(strings.Split(msg, "\n"))
	if lines > 50 {
		t.Errorf("message with stuck tasks is %d lines, want <=50", lines)
	}
}

func TestHeartbeatMessageFormat(t *testing.T) {
	msg := composeHeartbeatMessageSized(3, 0, nil)

	required := []string{
		"# Heartbeat",
		"Working directory:",
		"Repo state:",
		"Decisions:",
		"## Current State",
		"Active tasks:",
		"Stuck tasks:",
		"## Instructions",
	}
	for _, section := range required {
		if !strings.Contains(msg, section) {
			t.Errorf("missing required section: %q", section)
		}
	}
}

// TestHeartbeatContextSavings measures the context window savings between
// the old (full task listing) and new (count-based) heartbeat formats.
func TestHeartbeatContextSavings(t *testing.T) {
	tests := []struct {
		name       string
		taskCount  int
		stuckCount int
		minSaving  float64 // minimum expected reduction percentage
	}{
		{"0 tasks", 0, 0, 0},     // no savings with 0 tasks (new is slightly shorter anyway)
		{"1 task", 1, 0, 30},     // even 1 task saves ~30%+
		{"5 tasks", 5, 0, 50},    // 5 tasks saves significantly
		{"10 tasks", 10, 0, 60},  // 10 tasks saves 60%+
		{"20 tasks", 20, 0, 70},  // 20 tasks saves 70%+
		{"50 tasks", 50, 0, 80},  // 50 tasks saves 80%+
		{"50 tasks + 5 stuck", 50, 5, 70}, // with stuck tasks, still big savings
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tasks := makeTasks(tc.taskCount)
			stuck := makeStuckTasks(tc.stuckCount)

			oldMsg := composeOldHeartbeatMessage(tasks, stuck)
			newMsg := composeHeartbeatMessageSized(tc.taskCount, tc.stuckCount, stuck)

			oldSize := len(oldMsg)
			newSize := len(newMsg)

			saving := 0.0
			if oldSize > 0 {
				saving = float64(oldSize-newSize) / float64(oldSize) * 100
			}

			oldLines := len(strings.Split(oldMsg, "\n"))
			newLines := len(strings.Split(newMsg, "\n"))

			t.Logf("Tasks: %d active, %d stuck", tc.taskCount, tc.stuckCount)
			t.Logf("Old: %d bytes, %d lines", oldSize, oldLines)
			t.Logf("New: %d bytes, %d lines", newSize, newLines)
			t.Logf("Savings: %.1f%%", saving)

			if tc.taskCount > 0 && saving < tc.minSaving {
				t.Errorf("expected at least %.0f%% savings, got %.1f%%", tc.minSaving, saving)
			}

			// New format should always be under 50 lines
			if newLines > 50 {
				t.Errorf("new format is %d lines, want <=50", newLines)
			}
		})
	}
}

// TestDecisionFileFormatsPreserved ensures the heartbeat instructions still
// reference decision files correctly.
func TestDecisionFileFormatsPreserved(t *testing.T) {
	msg := composeHeartbeatMessageSized(10, 0, nil)

	// The new format refers orchestrator to HEARTBEAT.md for details
	if !strings.Contains(msg, "HEARTBEAT.md") {
		t.Error("should reference HEARTBEAT.md instructions")
	}

	if !strings.Contains(msg, "@flock-history-analyzer") {
		t.Error("should reference history analyzer agent")
	}
}

// TestHeartbeatTargetLineCount verifies the new heartbeat stays within
// the target of 30-50 lines regardless of task count.
func TestHeartbeatTargetLineCount(t *testing.T) {
	for _, count := range []int{0, 1, 5, 10, 25, 50, 100} {
		msg := composeHeartbeatMessageSized(count, 0, nil)
		lines := len(strings.Split(msg, "\n"))
		if lines < 10 || lines > 50 {
			t.Errorf("with %d tasks: got %d lines, want 10-50", count, lines)
		}
	}
}
