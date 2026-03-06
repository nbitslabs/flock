-- name: ListInstances :many
SELECT * FROM instances ORDER BY created_at DESC;

-- name: GetInstance :one
SELECT * FROM instances WHERE id = ?;

-- name: CreateInstance :one
INSERT INTO instances (id, pid, port, working_directory, status)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateInstanceStatus :exec
UPDATE instances SET status = ?, updated_at = datetime('now') WHERE id = ?;

-- name: UpdateInstancePort :exec
UPDATE instances SET port = ?, updated_at = datetime('now') WHERE id = ?;

-- name: DeleteInstance :exec
DELETE FROM instances WHERE id = ?;

-- name: MarkStaleInstancesStopped :exec
UPDATE instances SET status = 'stopped', updated_at = datetime('now') WHERE status IN ('starting', 'running');

-- name: ListSessionsByInstance :many
SELECT * FROM sessions WHERE instance_id = ? ORDER BY created_at DESC;

-- name: ListSessionsForRestore :many
SELECT id, instance_id, title, status FROM sessions WHERE instance_id = ? AND status != 'stopped' ORDER BY created_at DESC;

-- name: GetSession :one
SELECT * FROM sessions WHERE id = ?;

-- name: CreateSession :one
INSERT INTO sessions (id, instance_id, title, status)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: UpsertSession :exec
INSERT INTO sessions (id, instance_id, title, status)
VALUES (?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  title = excluded.title,
  updated_at = datetime('now');

-- name: UpdateSessionStatus :exec
UPDATE sessions SET status = ?, updated_at = datetime('now') WHERE id = ?;

-- name: DeleteSessionsByInstance :exec
DELETE FROM sessions WHERE instance_id = ?;
