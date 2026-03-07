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

-- name: GetInstanceHeartbeatHash :one
SELECT heartbeat_hash FROM instances WHERE id = ?;

-- name: UpdateInstanceHeartbeatHash :exec
UPDATE instances SET heartbeat_hash = ?, updated_at = datetime('now') WHERE id = ?;

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

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;

-- Task queries

-- name: CreateTask :one
INSERT INTO tasks (id, instance_id, issue_number, issue_url, title, status, session_id, branch_name)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetTaskByIssue :one
SELECT * FROM tasks WHERE instance_id = ? AND issue_number = ?;

-- name: GetTaskByID :one
SELECT * FROM tasks WHERE id = ?;

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
SET heartbeat_count = heartbeat_count + 1, last_heartbeat_at = datetime('now', 'utc'), updated_at = datetime('now', 'utc')
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

-- Flock agent session queries

-- name: CreateFlockAgentSession :one
INSERT INTO flock_agent_sessions (id, session_id, working_directory, status)
VALUES (?, ?, ?, 'active')
RETURNING *;

-- name: GetFlockAgentSession :one
SELECT * FROM flock_agent_sessions WHERE id = ?;

-- name: GetActiveFlockAgentSession :one
SELECT * FROM flock_agent_sessions WHERE status = 'active' LIMIT 1;

-- name: UpdateFlockAgentSession :exec
UPDATE flock_agent_sessions SET session_id = ?, status = ?, updated_at = datetime('now') WHERE id = ?;

-- name: RetireFlockAgentSession :exec
UPDATE flock_agent_sessions SET status = 'retired', updated_at = datetime('now') WHERE id = ?;
