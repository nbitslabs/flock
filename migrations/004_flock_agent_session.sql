-- +goose Up
CREATE TABLE flock_agent_sessions (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    working_directory TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- +goose Down
DROP TABLE flock_agent_sessions;
