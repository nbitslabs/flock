-- +goose Up

-- Repository manifest configuration for multi-repo coordination
CREATE TABLE IF NOT EXISTS repo_manifests (
    id TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL,
    org TEXT NOT NULL,
    repo TEXT NOT NULL,
    group_name TEXT NOT NULL,
    manifest_json TEXT NOT NULL,
    valid BOOLEAN NOT NULL DEFAULT TRUE,
    validation_error TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now', 'utc')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'utc')),
    UNIQUE(instance_id, org, repo)
);

CREATE INDEX idx_repo_manifests_group ON repo_manifests(group_name);
CREATE INDEX idx_repo_manifests_org_repo ON repo_manifests(org, repo);

-- Cross-repository task relationships
CREATE TABLE IF NOT EXISTS cross_repo_tasks (
    id TEXT PRIMARY KEY,
    parent_task_id TEXT NOT NULL,
    child_task_id TEXT NOT NULL,
    child_instance_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at DATETIME NOT NULL DEFAULT (datetime('now', 'utc')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'utc'))
);

CREATE INDEX idx_cross_repo_tasks_parent ON cross_repo_tasks(parent_task_id);
CREATE INDEX idx_cross_repo_tasks_child ON cross_repo_tasks(child_task_id);

-- Coordinated PR sets
CREATE TABLE IF NOT EXISTS pr_sets (
    id TEXT PRIMARY KEY,
    group_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'open',
    deployment_order TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now', 'utc')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now', 'utc'))
);

CREATE TABLE IF NOT EXISTS pr_set_members (
    id TEXT PRIMARY KEY,
    pr_set_id TEXT NOT NULL REFERENCES pr_sets(id),
    instance_id TEXT NOT NULL,
    org TEXT NOT NULL,
    repo TEXT NOT NULL,
    pr_url TEXT NOT NULL,
    pr_number INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'open',
    merge_order INTEGER NOT NULL DEFAULT 0,
    merged_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now', 'utc'))
);

CREATE INDEX idx_pr_set_members_set ON pr_set_members(pr_set_id);

-- +goose Down
DROP TABLE IF EXISTS pr_set_members;
DROP TABLE IF EXISTS pr_sets;
DROP TABLE IF EXISTS cross_repo_tasks;
DROP TABLE IF EXISTS repo_manifests;
