-- +goose Up
CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    issue_number INTEGER NOT NULL,
    issue_url TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    session_id TEXT NOT NULL DEFAULT '',
    branch_name TEXT NOT NULL DEFAULT '',
    pr_url TEXT NOT NULL DEFAULT '',
    last_activity_at TEXT NOT NULL DEFAULT (datetime('now')),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(instance_id, issue_number)
);

CREATE TABLE orchestrator_sessions (
    id TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL DEFAULT '',
    heartbeat_count INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- +goose Down
DROP TABLE orchestrator_sessions;
DROP TABLE tasks;
