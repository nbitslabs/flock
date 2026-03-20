-- +goose Up
CREATE VIRTUAL TABLE memory_search_index USING fts5(
    path,
    title,
    category,
    tags,
    content,
    tokenize='porter unicode61'
);

CREATE TABLE memory_index_meta (
    path TEXT PRIMARY KEY,
    category TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    tags TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    indexed_at TEXT NOT NULL DEFAULT (datetime('now', 'utc')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now', 'utc'))
);

-- +goose Down
DROP TABLE memory_index_meta;
DROP TABLE memory_search_index;
