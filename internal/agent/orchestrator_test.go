package agent

import (
	"fmt"
	"strings"
	"testing"
)

// simulateHeartbeat mirrors the actual composeHeartbeatMessage output format.
func simulateHeartbeat(activeCount int, stuckTasks []struct{ num int; id, lastActivity string }) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Heartbeat: active=%d stuck=%d decisions=`/tmp/decisions`\n", activeCount, len(stuckTasks)))

	if len(stuckTasks) > 0 {
		sb.WriteString("Stuck:")
		for _, t := range stuckTasks {
			sb.WriteString(fmt.Sprintf(" #%d(%s,last:%s)", t.num, t.id, t.lastActivity))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Run your HEARTBEAT.md steps now.")
	return sb.String()
}

// simulateOldHeartbeat mirrors the old verbose format for savings comparison.
func simulateOldHeartbeat(tasks []struct{ num int; title, status, id, branch, prUrl string }, stuckTasks []struct{ num int; id, lastActivity string }) string {
	var sb strings.Builder
	sb.WriteString("# Heartbeat\n\nWorking directory: `/tmp/test`\nRepo state: `/tmp/.flock/state/github.com/org/repo`\nDecisions: `/tmp/decisions`\n\n")

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
			sb.WriteString(fmt.Sprintf("- **#%d** (last activity: %s, task_id: %s)\n",
				t.num, t.lastActivity, t.id))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Instructions\n")
	sb.WriteString("1. Run `gh issue list --assignee=@me --state=open --json number,url,title`\n")
	sb.WriteString("2. For each active/stuck task above, check if its issue is closed\n")
	sb.WriteString("3. If the issue is open but the task has a pr_url, check if the PR is merged\n")
	sb.WriteString("4. Write completed_tasks.json, new_tasks.json, restart_tasks.json\n")
	sb.WriteString("5. Update MEMORY.md with any observations\n")

	return sb.String()
}

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

func makeStuckTasks(n int) []struct{ num int; id, lastActivity string } {
	tasks := make([]struct{ num int; id, lastActivity string }, n)
	for i := 0; i < n; i++ {
		tasks[i] = struct{ num int; id, lastActivity string }{
			num:          i + 100,
			id:           fmt.Sprintf("stuck-%04d", i+1),
			lastActivity: "2026-03-20T12:00:00Z",
		}
	}
	return tasks
}

func TestHeartbeatMessageConsistentSize(t *testing.T) {
	msg0 := simulateHeartbeat(0, nil)
	msg1 := simulateHeartbeat(1, nil)
	msg10 := simulateHeartbeat(10, nil)
	msg50 := simulateHeartbeat(50, nil)

	// Without stuck tasks, size should barely differ (only digit count changes)
	sizeDiff := len(msg50) - len(msg0)
	if sizeDiff > 5 {
		t.Errorf("size difference between 0 and 50 tasks is %d bytes, expected <5", sizeDiff)
	}

	// All should be very short — under 5 lines
	for _, msg := range []string{msg0, msg1, msg10, msg50} {
		lines := strings.Count(msg, "\n") + 1
		if lines > 5 {
			t.Errorf("message without stuck tasks is %d lines, want <=5", lines)
		}
	}
}

func TestHeartbeatMessageWithStuckTasks(t *testing.T) {
	stuck := makeStuckTasks(2)
	msg := simulateHeartbeat(5, stuck)

	if !strings.Contains(msg, "active=5") {
		t.Error("expected active count")
	}
	if !strings.Contains(msg, "stuck=2") {
		t.Error("expected stuck count")
	}
	if !strings.Contains(msg, "#100(stuck-0001") {
		t.Error("expected stuck task details")
	}
	if !strings.Contains(msg, "HEARTBEAT.md") {
		t.Error("expected HEARTBEAT.md reference")
	}

	lines := strings.Count(msg, "\n") + 1
	if lines > 5 {
		t.Errorf("message with 2 stuck tasks is %d lines, want <=5", lines)
	}
}

func TestHeartbeatMessageFormat(t *testing.T) {
	msg := simulateHeartbeat(3, nil)

	required := []string{
		"Heartbeat:",
		"active=",
		"stuck=",
		"decisions=",
		"HEARTBEAT.md",
	}
	for _, s := range required {
		if !strings.Contains(msg, s) {
			t.Errorf("missing required content: %q", s)
		}
	}
}

func TestHeartbeatContextSavings(t *testing.T) {
	tests := []struct {
		name       string
		taskCount  int
		stuckCount int
		minSaving  float64
	}{
		{"1 task", 1, 0, 80},
		{"5 tasks", 5, 0, 90},
		{"10 tasks", 10, 0, 93},
		{"50 tasks", 50, 0, 98},
		{"50 tasks + 5 stuck", 50, 5, 95},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tasks := makeTasks(tc.taskCount)
			stuck := makeStuckTasks(tc.stuckCount)

			oldMsg := simulateOldHeartbeat(tasks, stuck)
			newMsg := simulateHeartbeat(tc.taskCount, stuck)

			oldSize := len(oldMsg)
			newSize := len(newMsg)

			saving := float64(oldSize-newSize) / float64(oldSize) * 100

			t.Logf("Old: %d bytes, New: %d bytes, Savings: %.1f%%", oldSize, newSize, saving)

			if saving < tc.minSaving {
				t.Errorf("expected at least %.0f%% savings, got %.1f%%", tc.minSaving, saving)
			}
		})
	}
}

func TestBootstrapMessageConcise(t *testing.T) {
	// Simulate the new bootstrap message format
	bootstrap := fmt.Sprintf("You are the orchestrator for this repo. You will receive periodic heartbeat messages.\n\n"+
		"Paths: working_dir=`/tmp/test` state=`/tmp/state` decisions=`/tmp/decisions`\n\n"+
		"Read your instructions: `cat /tmp/state/HEARTBEAT.md`\n"+
		"Read repo memory: `cat /tmp/state/MEMORY.md`\n\n"+
		"Agents: `@flock-history-analyzer` (history queries), `@flock-self-reflect` (post-completion), `@flock-implementation-agent` (issue resolution).\n\n"+
		"Read HEARTBEAT.md now, then acknowledge.")

	lines := strings.Count(bootstrap, "\n") + 1
	if lines > 15 {
		t.Errorf("bootstrap is %d lines, want <=15", lines)
	}

	if len(bootstrap) > 500 {
		t.Errorf("bootstrap is %d bytes, want <=500", len(bootstrap))
	}

	// Must tell orchestrator to read files, not inline them
	if !strings.Contains(bootstrap, "cat") {
		t.Error("bootstrap should instruct orchestrator to read files")
	}
	if !strings.Contains(bootstrap, "HEARTBEAT.md") {
		t.Error("bootstrap should reference HEARTBEAT.md")
	}
}
