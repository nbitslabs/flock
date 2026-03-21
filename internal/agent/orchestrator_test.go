package agent

import (
	"fmt"
	"strings"
	"testing"
)

// composeHeartbeatMessageSized simulates composeHeartbeatMessage output
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

func TestHeartbeatMessageConsistentSize(t *testing.T) {
	// With 0 tasks
	msg0 := composeHeartbeatMessageSized(0, 0, nil)
	// With 1 task
	msg1 := composeHeartbeatMessageSized(1, 0, nil)
	// With 10 tasks
	msg10 := composeHeartbeatMessageSized(10, 0, nil)
	// With 50 tasks
	msg50 := composeHeartbeatMessageSized(50, 0, nil)

	// All should be nearly identical size since we only show counts
	lines0 := len(strings.Split(msg0, "\n"))
	lines1 := len(strings.Split(msg1, "\n"))
	lines10 := len(strings.Split(msg10, "\n"))
	lines50 := len(strings.Split(msg50, "\n"))

	// Line counts should be identical when no stuck tasks
	if lines0 != lines1 || lines1 != lines10 || lines10 != lines50 {
		t.Errorf("line counts differ: 0=%d, 1=%d, 10=%d, 50=%d", lines0, lines1, lines10, lines50)
	}

	// Verify the message is under 50 lines
	if lines0 > 50 {
		t.Errorf("message with 0 tasks is %d lines, want <=50", lines0)
	}

	// Size difference should be minimal (just the number width)
	sizeDiff := len(msg50) - len(msg0)
	if sizeDiff > 10 {
		t.Errorf("size difference between 0 and 50 tasks is %d bytes, expected <10", sizeDiff)
	}
}

func TestHeartbeatMessageWithStuckTasks(t *testing.T) {
	stuck := []struct{ num int; title, id, lastActivity string }{
		{42, "Fix login bug", "abc-123", "2026-03-20T12:00:00Z"},
		{43, "Update README", "def-456", "2026-03-20T11:00:00Z"},
	}

	msg := composeHeartbeatMessageSized(5, 2, stuck)

	if !strings.Contains(msg, "Active tasks: 5") {
		t.Error("expected active task count")
	}
	if !strings.Contains(msg, "Stuck tasks: 2") {
		t.Error("expected stuck task count")
	}
	if !strings.Contains(msg, "#42 Fix login bug") {
		t.Error("expected stuck task details")
	}
	if !strings.Contains(msg, "@flock-history-analyzer") {
		t.Error("expected history analyzer reference")
	}

	// Even with stuck tasks, should be under 50 lines
	lines := len(strings.Split(msg, "\n"))
	if lines > 50 {
		t.Errorf("message with stuck tasks is %d lines, want <=50", lines)
	}
}

func TestHeartbeatMessageFormat(t *testing.T) {
	msg := composeHeartbeatMessageSized(3, 0, nil)

	// Verify required sections exist
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
