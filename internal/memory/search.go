package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SearchIndexer manages the FTS5 search index for memory files.
type SearchIndexer struct {
	db      *sql.DB
	dataDir string
}

// NewSearchIndexer creates a new SearchIndexer.
func NewSearchIndexer(db *sql.DB, dataDir string) *SearchIndexer {
	return &SearchIndexer{db: db, dataDir: dataDir}
}

// SearchResult represents a single search result from the FTS index.
type SearchResult struct {
	Path     string  `json:"path"`
	Title    string  `json:"title"`
	Category string  `json:"category"`
	Tags     string  `json:"tags"`
	Snippet  string  `json:"snippet"`
	Rank     float64 `json:"rank"`
}

// IndexFile indexes a single memory file into the FTS5 search index.
func (si *SearchIndexer) IndexFile(ctx context.Context, filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	contentStr := string(content)
	contentHash := hashContent(contentStr)

	// Check if already indexed with same content
	var existingHash string
	err = si.db.QueryRowContext(ctx,
		"SELECT content_hash FROM memory_index_meta WHERE path = ?", filePath,
	).Scan(&existingHash)
	if err == nil && existingHash == contentHash {
		return nil // already up to date
	}

	fm, _ := ParseFrontmatter(contentStr)
	title := extractTitleFromContent(contentStr)
	category := fm["category"]
	tags := fm["tags"]

	// Strip frontmatter from content for indexing
	cleanContent := stripFrontmatter(contentStr)

	tx, err := si.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Remove old entry if exists
	tx.ExecContext(ctx, "DELETE FROM memory_search_index WHERE path = ?", filePath)
	tx.ExecContext(ctx, "DELETE FROM memory_index_meta WHERE path = ?", filePath)

	// Insert into FTS index
	_, err = tx.ExecContext(ctx,
		"INSERT INTO memory_search_index (path, title, category, tags, content) VALUES (?, ?, ?, ?, ?)",
		filePath, title, category, tags, cleanContent)
	if err != nil {
		return fmt.Errorf("insert fts: %w", err)
	}

	// Insert metadata
	_, err = tx.ExecContext(ctx,
		"INSERT INTO memory_index_meta (path, category, title, tags, content_hash) VALUES (?, ?, ?, ?, ?)",
		filePath, category, title, tags, contentHash)
	if err != nil {
		return fmt.Errorf("insert meta: %w", err)
	}

	return tx.Commit()
}

// RemoveFile removes a file from the search index.
func (si *SearchIndexer) RemoveFile(ctx context.Context, filePath string) error {
	tx, err := si.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tx.ExecContext(ctx, "DELETE FROM memory_search_index WHERE path = ?", filePath)
	tx.ExecContext(ctx, "DELETE FROM memory_index_meta WHERE path = ?", filePath)

	return tx.Commit()
}

// Search performs a full-text search on the memory index.
func (si *SearchIndexer) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}

	// Use FTS5 match syntax
	rows, err := si.db.QueryContext(ctx, `
		SELECT path, title, category, tags,
			snippet(memory_search_index, 4, '<mark>', '</mark>', '...', 32) as snippet,
			rank
		FROM memory_search_index
		WHERE memory_search_index MATCH ?
		ORDER BY rank
		LIMIT ?
	`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.Path, &r.Title, &r.Category, &r.Tags, &r.Snippet, &r.Rank); err != nil {
			continue
		}
		results = append(results, r)
	}

	return results, rows.Err()
}

// SearchByCategory searches within a specific category.
func (si *SearchIndexer) SearchByCategory(ctx context.Context, query, category string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}

	ftsQuery := query
	if category != "" {
		ftsQuery = fmt.Sprintf("category:%s %s", category, query)
	}

	return si.Search(ctx, ftsQuery, limit)
}

// RebuildIndex scans all memory files and rebuilds the entire search index.
func (si *SearchIndexer) RebuildIndex(ctx context.Context) (int, error) {
	// Clear existing index
	si.db.ExecContext(ctx, "DELETE FROM memory_search_index")
	si.db.ExecContext(ctx, "DELETE FROM memory_index_meta")

	stateDir := ResolveStateDir(si.dataDir)
	count := 0

	err := filepath.Walk(stateDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		if info.Name() == "HEARTBEAT.md" {
			return nil
		}

		if err := si.IndexFile(ctx, path); err != nil {
			return nil // skip errors for individual files
		}
		count++
		return nil
	})

	return count, err
}

// hashContent returns a SHA256 hash of the content.
func hashContent(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// extractTitleFromContent extracts the first # heading.
func extractTitleFromContent(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "## ") {
			return strings.TrimPrefix(trimmed, "# ")
		}
	}
	return ""
}

// stripFrontmatter removes YAML frontmatter from markdown content.
func stripFrontmatter(content string) string {
	if !strings.HasPrefix(strings.TrimSpace(content), "---") {
		return content
	}

	trimmed := strings.TrimSpace(content)
	rest := trimmed[3:]
	endIdx := strings.Index(rest, "\n---")
	if endIdx == -1 {
		return content
	}

	return strings.TrimSpace(rest[endIdx+4:])
}
