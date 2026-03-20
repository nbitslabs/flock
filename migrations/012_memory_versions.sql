-- +goose Up
CREATE TABLE memory_versions (
    id TEXT PRIMARY KEY,
    path TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL DEFAULT '',
    author TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now', 'utc')),
    UNIQUE(path, version)
);

CREATE INDEX idx_memory_versions_path ON memory_versions(path);

-- +goose Down
DROP TABLE memory_versions;
