-- +goose Up
ALTER TABLE instances ADD COLUMN heartbeat_hash TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE instances DROP COLUMN heartbeat_hash;
