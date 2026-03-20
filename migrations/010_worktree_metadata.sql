-- +goose Up
CREATE TABLE worktree_metadata (
    id TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    branch_name TEXT NOT NULL,
    worktree_path TEXT NOT NULL,
    issue_number INTEGER NOT NULL DEFAULT 0,
    agent_session_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',  -- active, abandoned, completed, failed, corrupted
    deletion_reason TEXT NOT NULL DEFAULT '',  -- completed, abandoned, failed, manual
    disk_usage_bytes INTEGER NOT NULL DEFAULT 0,
    has_uncommitted_changes INTEGER NOT NULL DEFAULT 0,
    last_activity_at TEXT NOT NULL DEFAULT (datetime('now', 'utc')),
    created_at TEXT NOT NULL DEFAULT (datetime('now', 'utc')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now', 'utc')),
    deleted_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE worktree_health_checks (
    id TEXT PRIMARY KEY,
    worktree_id TEXT NOT NULL REFERENCES worktree_metadata(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'healthy',  -- healthy, warning, corrupted, missing
    git_fsck_ok INTEGER NOT NULL DEFAULT 1,
    has_uncommitted_changes INTEGER NOT NULL DEFAULT 0,
    disk_usage_bytes INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    checked_at TEXT NOT NULL DEFAULT (datetime('now', 'utc'))
);

CREATE INDEX idx_worktree_metadata_instance ON worktree_metadata(instance_id);
CREATE INDEX idx_worktree_metadata_branch ON worktree_metadata(branch_name);
CREATE INDEX idx_worktree_metadata_status ON worktree_metadata(status);
CREATE INDEX idx_worktree_health_checks_worktree ON worktree_health_checks(worktree_id);

-- +goose Down
DROP TABLE worktree_health_checks;
DROP TABLE worktree_metadata;
