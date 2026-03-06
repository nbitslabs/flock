-- +goose Up
ALTER TABLE orchestrator_sessions ADD COLUMN last_heartbeat_at TEXT NOT NULL DEFAULT (datetime('now'));

-- +goose Down
ALTER TABLE orchestrator_sessions DROP COLUMN last_heartbeat_at;
