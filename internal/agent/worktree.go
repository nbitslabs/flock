package agent

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/nbitslabs/flock/internal/memory"
)

// EnsureWorktree creates a git worktree at the repo-based worktree path if it
// doesn't already exist. It returns the absolute path to the worktree directory.
// sourceRepoPath is the path to the main git repository checkout.
func EnsureWorktree(dataDir, org, repo, branchName, sourceRepoPath string) (string, error) {
	wtPath := memory.RepoWorktreePath(dataDir, org, repo, branchName)

	// If the worktree directory already exists, just return it
	if info, err := os.Stat(wtPath); err == nil && info.IsDir() {
		log.Printf("agent: worktree already exists at %s", wtPath)
		return wtPath, nil
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(memory.RepoStatePath(dataDir, org, repo)+"/worktrees", 0o755); err != nil {
		return "", fmt.Errorf("mkdir worktrees: %w", err)
	}

	// Create the worktree
	cmd := exec.Command("git", "worktree", "add", "-b", branchName, wtPath)
	cmd.Dir = sourceRepoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		// If the branch already exists, try without -b
		cmd2 := exec.Command("git", "worktree", "add", wtPath, branchName)
		cmd2.Dir = sourceRepoPath
		output2, err2 := cmd2.CombinedOutput()
		if err2 != nil {
			return "", fmt.Errorf("git worktree add failed: %w\nfirst attempt: %s\nsecond attempt: %s", err2, string(output), string(output2))
		}
	}

	log.Printf("agent: created worktree at %s for branch %s", wtPath, branchName)
	return wtPath, nil
}

// RemoveWorktree removes a git worktree at the repo-based path.
func RemoveWorktree(dataDir, org, repo, branchName string) error {
	wtPath := memory.RepoWorktreePath(dataDir, org, repo, branchName)

	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		return nil
	}

	cmd := exec.Command("git", "worktree", "remove", "--force", wtPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove failed: %w, output: %s", err, string(output))
	}

	log.Printf("agent: removed worktree at %s", wtPath)
	return nil
}
