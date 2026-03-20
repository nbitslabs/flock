package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/nbitslabs/flock/internal/db/sqlc"
)

// HealthChecker runs periodic health checks on active worktrees.
type HealthChecker struct {
	queries    *sqlc.Queries
	instanceID string
	dataDir    string
}

// NewHealthChecker creates a new HealthChecker.
func NewHealthChecker(queries *sqlc.Queries, instanceID, dataDir string) *HealthChecker {
	return &HealthChecker{
		queries:    queries,
		instanceID: instanceID,
		dataDir:    dataDir,
	}
}

// HealthCheckResult contains the results of a single worktree health check.
type HealthCheckResult struct {
	WorktreeID          string
	Status              string // healthy, warning, corrupted, missing
	GitFsckOK           bool
	HasUncommittedChanges bool
	DiskUsageBytes      int64
	ErrorMessage        string
}

// RunHealthChecks checks all active worktrees for the instance.
func (hc *HealthChecker) RunHealthChecks(ctx context.Context) []HealthCheckResult {
	worktrees, err := hc.queries.ListActiveWorktrees(ctx, hc.instanceID)
	if err != nil {
		log.Printf("agent: healthcheck: failed to list active worktrees: %v", err)
		return nil
	}

	var results []HealthCheckResult
	for _, wt := range worktrees {
		result := hc.checkWorktree(wt)
		results = append(results, result)

		// Store health check in DB
		hc.queries.CreateHealthCheck(ctx, sqlc.CreateHealthCheckParams{
			ID:                    uuid.New().String(),
			WorktreeID:            wt.ID,
			Status:                result.Status,
			GitFsckOk:             boolToInt(result.GitFsckOK),
			HasUncommittedChanges: boolToInt(result.HasUncommittedChanges),
			DiskUsageBytes:        result.DiskUsageBytes,
			ErrorMessage:          result.ErrorMessage,
		})

		// Update worktree metadata
		hc.queries.UpdateWorktreeDiskUsage(ctx, sqlc.UpdateWorktreeDiskUsageParams{
			DiskUsageBytes:        result.DiskUsageBytes,
			HasUncommittedChanges: boolToInt(result.HasUncommittedChanges),
			ID:                    wt.ID,
		})

		if result.Status == "corrupted" {
			hc.queries.UpdateWorktreeStatus(ctx, sqlc.UpdateWorktreeStatusParams{
				Status: "corrupted",
				ID:     wt.ID,
			})
		}
	}

	return results
}

// checkWorktree performs health checks on a single worktree.
func (hc *HealthChecker) checkWorktree(wt sqlc.WorktreeMetadatum) HealthCheckResult {
	result := HealthCheckResult{
		WorktreeID: wt.ID,
		Status:     "healthy",
		GitFsckOK:  true,
	}

	wtPath := wt.WorktreePath

	// Check if worktree directory exists
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		result.Status = "missing"
		result.ErrorMessage = "worktree directory does not exist"
		log.Printf("agent: healthcheck: worktree %s is missing at %s", truncID(wt.ID), wtPath)
		return result
	}

	// Check for .git file (worktree marker)
	gitPath := filepath.Join(wtPath, ".git")
	if info, err := os.Stat(gitPath); err != nil || info.IsDir() {
		result.Status = "corrupted"
		result.GitFsckOK = false
		result.ErrorMessage = "missing or invalid .git file (not a valid worktree)"
		log.Printf("agent: healthcheck: worktree %s has invalid .git at %s", truncID(wt.ID), wtPath)
		return result
	}

	// Run git fsck
	if ok, errMsg := runGitFsck(wtPath); !ok {
		result.Status = "corrupted"
		result.GitFsckOK = false
		result.ErrorMessage = fmt.Sprintf("git fsck failed: %s", errMsg)
		log.Printf("agent: healthcheck: git fsck failed for worktree %s: %s", truncID(wt.ID), errMsg)
	}

	// Check for uncommitted changes
	if has, err := hasUncommittedChanges(wtPath); err != nil {
		log.Printf("agent: healthcheck: failed to check uncommitted changes for %s: %v", truncID(wt.ID), err)
	} else {
		result.HasUncommittedChanges = has
		if has && result.Status == "healthy" {
			result.Status = "warning"
		}
	}

	// Calculate disk usage
	if size, err := dirSize(wtPath); err != nil {
		log.Printf("agent: healthcheck: failed to get disk usage for %s: %v", truncID(wt.ID), err)
	} else {
		result.DiskUsageBytes = size
		// Warn if over 1GB
		if size > 1024*1024*1024 {
			if result.Status == "healthy" {
				result.Status = "warning"
			}
			result.ErrorMessage += fmt.Sprintf("; disk usage %.1f GB exceeds 1 GB threshold", float64(size)/(1024*1024*1024))
		}
	}

	return result
}

// runGitFsck runs git fsck on a worktree and returns (ok, errorMessage).
func runGitFsck(worktreePath string) (bool, string) {
	cmd := exec.Command("git", "fsck", "--no-full", "--no-progress")
	cmd.Dir = worktreePath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, strings.TrimSpace(string(output))
	}
	return true, ""
}

// dirSize calculates the total size of a directory in bytes.
func dirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
