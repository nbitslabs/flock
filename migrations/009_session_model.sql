-- +goose Up
ALTER TABLE sessions ADD COLUMN model TEXT;

-- +goose Down
-- SQLite doesn't support DROP COLUMN directly, use table rewrite
CREATE TABLE sessions_backup (
    id TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL,
    title TEXT,
    status TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    parent_id TEXT
);
INSERT INTO sessions_backup SELECT id, instance_id, title, status, created_at, updated_at, parent_id FROM sessions;
DROP TABLE sessions;
ALTER TABLE sessions_backup RENAME TO sessions;
