-- name: ListInstances :many
SELECT * FROM instances ORDER BY created_at DESC;

-- name: GetInstance :one
SELECT * FROM instances WHERE id = ?;

-- name: CreateInstance :one
INSERT INTO instances (id, pid, port, working_directory, status)
VALUES (?, 0, 0, ?, 'running')
RETURNING *;

-- name: UpdateInstanceStatus :exec
UPDATE instances SET status = ?, updated_at = datetime('now') WHERE id = ?;

-- name: DeleteInstance :exec
DELETE FROM instances WHERE id = ?;

-- name: ListSessionsByInstance :many
SELECT * FROM sessions WHERE instance_id = ? ORDER BY created_at DESC;

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

-- Task queries

-- name: CreateTask :one
INSERT INTO tasks (id, instance_id, issue_number, issue_url, title, status, session_id, branch_name)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetTaskByIssue :one
SELECT * FROM tasks WHERE instance_id = ? AND issue_number = ?;

-- name: ListTasksByInstance :many
SELECT * FROM tasks WHERE instance_id = ? ORDER BY created_at DESC;

-- name: ListActiveTasks :many
SELECT * FROM tasks WHERE instance_id = ? AND status IN ('pending', 'active') ORDER BY created_at ASC;

-- name: UpdateTaskStatus :exec
UPDATE tasks SET status = ?, updated_at = datetime('now') WHERE id = ?;

-- name: UpdateTaskSession :exec
UPDATE tasks SET session_id = ?, updated_at = datetime('now') WHERE id = ?;

-- name: UpdateTaskActivity :exec
UPDATE tasks SET last_activity_at = datetime('now'), updated_at = datetime('now') WHERE id = ?;

-- name: UpdateTaskPR :exec
UPDATE tasks SET pr_url = ?, status = 'completed', updated_at = datetime('now') WHERE id = ?;

-- name: ListStuckTasks :many
SELECT * FROM tasks
WHERE instance_id = ? AND status = 'active'
AND last_activity_at < datetime('now', '-' || cast(? as text) || ' seconds');

-- name: DeleteTasksByInstance :exec
DELETE FROM tasks WHERE instance_id = ?;

-- Orchestrator session queries

-- name: CreateOrchestratorSession :one
INSERT INTO orchestrator_sessions (id, instance_id, session_id, status)
VALUES (?, ?, ?, 'active')
RETURNING *;

-- name: GetActiveOrchestratorSession :one
SELECT * FROM orchestrator_sessions
WHERE instance_id = ? AND status = 'active'
LIMIT 1;

-- name: IncrementHeartbeatCount :exec
UPDATE orchestrator_sessions
SET heartbeat_count = heartbeat_count + 1, last_heartbeat_at = datetime('now'), updated_at = datetime('now')
WHERE id = ?;

-- name: RetireOrchestratorSession :exec
UPDATE orchestrator_sessions
SET status = 'retired', updated_at = datetime('now')
WHERE id = ?;

-- name: DeleteOrchestratorSessionsByInstance :exec
DELETE FROM orchestrator_sessions WHERE instance_id = ?;

-- name: GetLastHeartbeatByInstance :one
SELECT last_heartbeat_at FROM orchestrator_sessions
WHERE instance_id = ? AND status = 'active'
ORDER BY last_heartbeat_at DESC
LIMIT 1;
