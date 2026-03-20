-- +goose Up

-- Historical test failures for pattern matching and root cause analysis
CREATE TABLE IF NOT EXISTS test_failures (
    id TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL,
    session_id TEXT,
    framework TEXT NOT NULL,
    test_name TEXT NOT NULL,
    file_path TEXT,
    assertion_text TEXT,
    stack_trace TEXT,
    input_values TEXT,
    error_message TEXT NOT NULL,
    code_changes TEXT,
    resolved BOOLEAN NOT NULL DEFAULT FALSE,
    resolved_at DATETIME,
    fix_description TEXT,
    fix_diff TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now', 'utc'))
);

CREATE INDEX idx_test_failures_instance ON test_failures(instance_id);
CREATE INDEX idx_test_failures_test_name ON test_failures(test_name);
CREATE INDEX idx_test_failures_framework ON test_failures(framework);
CREATE INDEX idx_test_failures_resolved ON test_failures(resolved);
CREATE INDEX idx_test_failures_created ON test_failures(created_at);

-- +goose Down
DROP TABLE IF EXISTS test_failures;
