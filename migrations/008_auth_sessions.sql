-- +goose Up
CREATE TABLE auth_sessions (
    token TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE auth_sessions;
