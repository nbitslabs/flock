package memory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// VersionManager handles memory file versioning.
type VersionManager struct {
	db *sql.DB
}

// NewVersionManager creates a new VersionManager.
func NewVersionManager(db *sql.DB) *VersionManager {
	return &VersionManager{db: db}
}

// MemoryVersion represents a single version of a memory file.
type MemoryVersion struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Version     int    `json:"version"`
	Content     string `json:"content"`
	ContentHash string `json:"content_hash"`
	Author      string `json:"author"`
	Reason      string `json:"reason"`
	CreatedAt   string `json:"created_at"`
}

// SaveVersion stores a new version of a memory file.
func (vm *VersionManager) SaveVersion(ctx context.Context, path, content, author, reason string) (*MemoryVersion, error) {
	contentHash := hashContent(content)

	// Get current latest version number
	var latestVersion int
	err := vm.db.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(version), 0) FROM memory_versions WHERE path = ?", path,
	).Scan(&latestVersion)
	if err != nil {
		return nil, fmt.Errorf("get latest version: %w", err)
	}

	// Check if content actually changed
	if latestVersion > 0 {
		var existingHash string
		vm.db.QueryRowContext(ctx,
			"SELECT content_hash FROM memory_versions WHERE path = ? AND version = ?",
			path, latestVersion,
		).Scan(&existingHash)
		if existingHash == contentHash {
			return nil, nil // no change
		}
	}

	newVersion := latestVersion + 1
	id := uuid.New().String()

	_, err = vm.db.ExecContext(ctx,
		`INSERT INTO memory_versions (id, path, version, content, content_hash, author, reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, path, newVersion, content, contentHash, author, reason)
	if err != nil {
		return nil, fmt.Errorf("insert version: %w", err)
	}

	return &MemoryVersion{
		ID:          id,
		Path:        path,
		Version:     newVersion,
		Content:     content,
		ContentHash: contentHash,
		Author:      author,
		Reason:      reason,
	}, nil
}

// GetHistory returns the version history for a memory file.
func (vm *VersionManager) GetHistory(ctx context.Context, path string) ([]MemoryVersion, error) {
	rows, err := vm.db.QueryContext(ctx,
		`SELECT id, path, version, content, content_hash, author, reason, created_at
		 FROM memory_versions WHERE path = ? ORDER BY version DESC`, path)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()

	var versions []MemoryVersion
	for rows.Next() {
		var v MemoryVersion
		if err := rows.Scan(&v.ID, &v.Path, &v.Version, &v.Content, &v.ContentHash, &v.Author, &v.Reason, &v.CreatedAt); err != nil {
			continue
		}
		versions = append(versions, v)
	}

	return versions, rows.Err()
}

// GetVersion returns a specific version of a memory file.
func (vm *VersionManager) GetVersion(ctx context.Context, path string, version int) (*MemoryVersion, error) {
	var v MemoryVersion
	err := vm.db.QueryRowContext(ctx,
		`SELECT id, path, version, content, content_hash, author, reason, created_at
		 FROM memory_versions WHERE path = ? AND version = ?`, path, version,
	).Scan(&v.ID, &v.Path, &v.Version, &v.Content, &v.ContentHash, &v.Author, &v.Reason, &v.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get version: %w", err)
	}
	return &v, nil
}

// Rollback reverts a memory file to a previous version.
func (vm *VersionManager) Rollback(ctx context.Context, path string, targetVersion int) (*MemoryVersion, error) {
	target, err := vm.GetVersion(ctx, path, targetVersion)
	if err != nil {
		return nil, fmt.Errorf("get target version: %w", err)
	}

	return vm.SaveVersion(ctx, path, target.Content, "system", fmt.Sprintf("rollback to version %d", targetVersion))
}

// DiffVersions generates a simple line-level diff between two versions.
func (vm *VersionManager) DiffVersions(ctx context.Context, path string, fromVersion, toVersion int) (string, error) {
	from, err := vm.GetVersion(ctx, path, fromVersion)
	if err != nil {
		return "", fmt.Errorf("get from version: %w", err)
	}

	to, err := vm.GetVersion(ctx, path, toVersion)
	if err != nil {
		return "", fmt.Errorf("get to version: %w", err)
	}

	return SimpleDiff(from.Content, to.Content), nil
}

// SimpleDiff generates a basic unified-style diff between two strings.
func SimpleDiff(old, new string) string {
	oldLines := strings.Split(old, "\n")
	newLines := strings.Split(new, "\n")

	var diff strings.Builder
	diff.WriteString(fmt.Sprintf("--- version A (%d lines)\n", len(oldLines)))
	diff.WriteString(fmt.Sprintf("+++ version B (%d lines)\n", len(newLines)))

	// Simple LCS-based diff
	maxLen := len(oldLines)
	if len(newLines) > maxLen {
		maxLen = len(newLines)
	}

	i, j := 0, 0
	for i < len(oldLines) || j < len(newLines) {
		if i < len(oldLines) && j < len(newLines) && oldLines[i] == newLines[j] {
			diff.WriteString("  " + oldLines[i] + "\n")
			i++
			j++
		} else if j < len(newLines) && (i >= len(oldLines) || !containsLine(oldLines[i:], newLines[j])) {
			diff.WriteString("+ " + newLines[j] + "\n")
			j++
		} else if i < len(oldLines) {
			diff.WriteString("- " + oldLines[i] + "\n")
			i++
		}
	}

	return diff.String()
}

func containsLine(lines []string, target string) bool {
	for _, l := range lines {
		if l == target {
			return true
		}
	}
	return false
}
