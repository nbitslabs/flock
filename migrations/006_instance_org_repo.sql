-- +goose Up
ALTER TABLE instances ADD COLUMN org TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN repo TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE instances DROP COLUMN org;
ALTER TABLE instances DROP COLUMN repo;
