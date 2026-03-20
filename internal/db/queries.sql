-- name: ListInstances :many
SELECT * FROM instances WHERE id != 'flock-agent' ORDER BY created_at DESC;

-- name: GetInstance :one
SELECT * FROM instances WHERE id = ?;

-- name: CreateInstance :one
INSERT INTO instances (id, pid, port, working_directory, status, org, repo)
VALUES (?, 0, 0, ?, 'running', ?, ?)
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
INSERT INTO sessions (id, instance_id, parent_id, title, status)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  title = excluded.title,
  updated_at = datetime('now');

-- name: UpdateSessionStatus :exec
UPDATE sessions SET status = ?, updated_at = datetime('now') WHERE id = ?;

-- name: UpdateSessionModel :exec
UPDATE sessions SET model = ?, updated_at = datetime('now') WHERE id = ?;

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

-- name: CountActiveTasksByInstance :one
SELECT COUNT(*) FROM tasks WHERE instance_id = ? AND status IN ('pending', 'active');

-- name: CountAllActiveTasks :one
SELECT COUNT(*) FROM tasks WHERE status IN ('pending', 'active');

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

-- name: ListCompletedTasks :many
SELECT * FROM tasks WHERE instance_id = ? AND status = 'completed' ORDER BY updated_at ASC;

-- name: ListFailedTasks :many
SELECT * FROM tasks WHERE instance_id = ? AND status IN ('failed', 'stuck') ORDER BY last_activity_at ASC;

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

-- name: EnsureFlockAgentInstance :exec
INSERT OR IGNORE INTO instances (id, pid, port, working_directory, status, org, repo)
VALUES ('flock-agent', 0, 0, '', 'running', '', '');

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

-- name: GetActiveFlockAgentSessionByInstance :one
SELECT * FROM sessions WHERE instance_id = 'flock-agent' AND status = 'active' LIMIT 1;

-- Worktree metadata queries

-- name: CreateWorktreeMetadata :one
INSERT INTO worktree_metadata (id, instance_id, branch_name, worktree_path, issue_number, agent_session_id)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetWorktreeByBranch :one
SELECT * FROM worktree_metadata
WHERE instance_id = ? AND branch_name = ? AND status = 'active'
LIMIT 1;

-- name: GetWorktreeByID :one
SELECT * FROM worktree_metadata WHERE id = ?;

-- name: ListActiveWorktrees :many
SELECT * FROM worktree_metadata
WHERE instance_id = ? AND status = 'active'
ORDER BY created_at ASC;

-- name: ListAllWorktrees :many
SELECT * FROM worktree_metadata
WHERE instance_id = ?
ORDER BY created_at DESC;

-- name: UpdateWorktreeActivity :exec
UPDATE worktree_metadata
SET last_activity_at = datetime('now', 'utc'), updated_at = datetime('now', 'utc')
WHERE id = ?;

-- name: UpdateWorktreeStatus :exec
UPDATE worktree_metadata
SET status = ?, updated_at = datetime('now', 'utc')
WHERE id = ?;

-- name: UpdateWorktreeDeleted :exec
UPDATE worktree_metadata
SET status = 'completed', deletion_reason = ?, deleted_at = datetime('now', 'utc'), updated_at = datetime('now', 'utc')
WHERE id = ?;

-- name: UpdateWorktreeDiskUsage :exec
UPDATE worktree_metadata
SET disk_usage_bytes = ?, has_uncommitted_changes = ?, updated_at = datetime('now', 'utc')
WHERE id = ?;

-- name: ListAbandonedWorktrees :many
SELECT * FROM worktree_metadata
WHERE instance_id = ? AND status = 'active'
AND last_activity_at < datetime('now', 'utc', '-24 hours')
ORDER BY last_activity_at ASC;

-- name: CreateHealthCheck :one
INSERT INTO worktree_health_checks (id, worktree_id, status, git_fsck_ok, has_uncommitted_changes, disk_usage_bytes, error_message)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListHealthChecksByWorktree :many
SELECT * FROM worktree_health_checks
WHERE worktree_id = ?
ORDER BY checked_at DESC
LIMIT 10;

-- name: GetLatestHealthCheck :one
SELECT * FROM worktree_health_checks
WHERE worktree_id = ?
ORDER BY checked_at DESC
LIMIT 1;

-- Auth session queries

-- name: CreateAuthSession :one
INSERT INTO auth_sessions (token, username, expires_at)
VALUES (?, ?, datetime('now', '+7 days'))
RETURNING *;

-- name: GetAuthSession :one
SELECT * FROM auth_sessions WHERE token = ? AND expires_at > datetime('now');

-- name: DeleteAuthSession :exec
DELETE FROM auth_sessions WHERE token = ?;

-- name: DeleteExpiredAuthSessions :exec
DELETE FROM auth_sessions WHERE expires_at <= datetime('now');

-- Test failure queries

-- name: CreateTestFailure :one
INSERT INTO test_failures (id, instance_id, session_id, framework, test_name, file_path, assertion_text, stack_trace, input_values, error_message, code_changes)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetTestFailure :one
SELECT * FROM test_failures WHERE id = ?;

-- name: ListTestFailuresByInstance :many
SELECT * FROM test_failures
WHERE instance_id = ?
ORDER BY created_at DESC
LIMIT ?;

-- name: ListUnresolvedFailures :many
SELECT * FROM test_failures
WHERE instance_id = ? AND resolved = FALSE
ORDER BY created_at DESC;

-- name: ListFailuresByTestName :many
SELECT * FROM test_failures
WHERE test_name = ?
ORDER BY created_at DESC
LIMIT ?;

-- name: ListFailuresByFramework :many
SELECT * FROM test_failures
WHERE instance_id = ? AND framework = ?
ORDER BY created_at DESC
LIMIT ?;

-- name: ResolveTestFailure :exec
UPDATE test_failures
SET resolved = TRUE, resolved_at = datetime('now', 'utc'), fix_description = ?, fix_diff = ?
WHERE id = ?;

-- name: ListResolvedFailures :many
SELECT * FROM test_failures
WHERE instance_id = ? AND resolved = TRUE
ORDER BY resolved_at DESC
LIMIT ?;

-- name: CountFailuresByTestName :one
SELECT COUNT(*) FROM test_failures
WHERE instance_id = ? AND test_name = ?;

-- Repository manifest queries

-- name: UpsertRepoManifest :one
INSERT INTO repo_manifests (id, instance_id, org, repo, group_name, manifest_json)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(instance_id, org, repo) DO UPDATE SET
  group_name = excluded.group_name,
  manifest_json = excluded.manifest_json,
  valid = TRUE,
  validation_error = NULL,
  updated_at = datetime('now', 'utc')
RETURNING *;

-- name: GetRepoManifest :one
SELECT * FROM repo_manifests WHERE instance_id = ? AND org = ? AND repo = ?;

-- name: GetRepoManifestByID :one
SELECT * FROM repo_manifests WHERE id = ?;

-- name: ListManifestsByGroup :many
SELECT * FROM repo_manifests WHERE group_name = ? ORDER BY org, repo;

-- name: ListAllManifests :many
SELECT * FROM repo_manifests ORDER BY group_name, org, repo;

-- name: UpdateManifestValidation :exec
UPDATE repo_manifests SET valid = ?, validation_error = ?, updated_at = datetime('now', 'utc') WHERE id = ?;

-- name: DeleteRepoManifest :exec
DELETE FROM repo_manifests WHERE id = ?;

-- Cross-repo task queries

-- name: CreateCrossRepoTask :one
INSERT INTO cross_repo_tasks (id, parent_task_id, child_task_id, child_instance_id)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: ListCrossRepoTasksByParent :many
SELECT * FROM cross_repo_tasks WHERE parent_task_id = ? ORDER BY created_at;

-- name: ListCrossRepoTasksByChild :many
SELECT * FROM cross_repo_tasks WHERE child_task_id = ?;

-- name: UpdateCrossRepoTaskStatus :exec
UPDATE cross_repo_tasks SET status = ?, updated_at = datetime('now', 'utc') WHERE id = ?;

-- PR set queries

-- name: CreatePRSet :one
INSERT INTO pr_sets (id, group_name, deployment_order)
VALUES (?, ?, ?)
RETURNING *;

-- name: GetPRSet :one
SELECT * FROM pr_sets WHERE id = ?;

-- name: ListPRSets :many
SELECT * FROM pr_sets WHERE group_name = ? ORDER BY created_at DESC;

-- name: UpdatePRSetStatus :exec
UPDATE pr_sets SET status = ?, updated_at = datetime('now', 'utc') WHERE id = ?;

-- name: CreatePRSetMember :one
INSERT INTO pr_set_members (id, pr_set_id, instance_id, org, repo, pr_url, pr_number, merge_order)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListPRSetMembers :many
SELECT * FROM pr_set_members WHERE pr_set_id = ? ORDER BY merge_order;

-- name: UpdatePRSetMemberStatus :exec
UPDATE pr_set_members SET status = ?, updated_at = datetime('now', 'utc') WHERE id = ?;

-- name: UpdatePRSetMemberMerged :exec
UPDATE pr_set_members SET status = 'merged', merged_at = datetime('now', 'utc') WHERE id = ?;
