package agent

import "time"

// AgentConfig holds configuration for the agent harness.
type AgentConfig struct {
	Enabled                  bool          `toml:"enabled"`
	HeartbeatInterval        time.Duration `toml:"-"`
	HeartbeatIntervalSecs    int           `toml:"heartbeat_interval_secs"`
	StuckThresholdSecs       int           `toml:"stuck_threshold_secs"`
	MaxHeartbeatsPerSession  int           `toml:"max_heartbeats_per_session"`
	WaitForIdleTimeout       time.Duration `toml:"-"`
	WaitForIdleTimeoutSecs   int           `toml:"wait_for_idle_timeout_secs"`
}

// DefaultAgentConfig returns sensible defaults.
func DefaultAgentConfig() AgentConfig {
	return AgentConfig{
		Enabled:                 false,
		HeartbeatIntervalSecs:   300,  // 5 minutes
		StuckThresholdSecs:      600,  // 10 minutes
		MaxHeartbeatsPerSession: 20,   // ~100 minutes before rotation
		WaitForIdleTimeoutSecs:  180,  // 3 minutes
	}
}

// Resolve computes duration fields from seconds fields.
func (c *AgentConfig) Resolve() {
	c.HeartbeatInterval = time.Duration(c.HeartbeatIntervalSecs) * time.Second
	if c.HeartbeatInterval == 0 {
		c.HeartbeatInterval = 5 * time.Minute
	}
	c.WaitForIdleTimeout = time.Duration(c.WaitForIdleTimeoutSecs) * time.Second
	if c.WaitForIdleTimeout == 0 {
		c.WaitForIdleTimeout = 3 * time.Minute
	}
}

// TaskSummary is a compact view of a task for heartbeat context.
type TaskSummary struct {
	ID          string `json:"id"`
	IssueNumber int64  `json:"issue_number"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	SessionID   string `json:"session_id,omitempty"`
	BranchName  string `json:"branch_name,omitempty"`
}
