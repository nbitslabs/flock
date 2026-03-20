package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// blockedGitSubcommands are git subcommands that sub-agents must never run
// because they could break worktree isolation.
var blockedGitSubcommands = map[string]string{
	"checkout": "Branch switching is not allowed — your branch is fixed to the worktree.",
	"switch":   "Branch switching is not allowed — your branch is fixed to the worktree.",
}

// blockedWorktreeActions are git worktree subcommands that are forbidden.
var blockedWorktreeActions = map[string]string{
	"add":    "Worktree creation is managed by flock. Do not create worktrees manually.",
	"remove": "Worktree removal is managed by flock. Do not remove worktrees manually.",
	"move":   "Worktree moves are managed by flock. Do not move worktrees manually.",
	"prune":  "Worktree pruning is managed by flock. Do not prune worktrees manually.",
}

// IsBlockedGitCommand checks whether a git command (given as CLI args after
// "git") should be blocked for sub-agents. It returns (blocked, message).
func IsBlockedGitCommand(args []string) (bool, string) {
	if len(args) == 0 {
		return false, ""
	}

	// Strip leading flags (e.g. git -C /path ...)
	idx := 0
	for idx < len(args) {
		if strings.HasPrefix(args[idx], "-") {
			idx++
			// Skip the value for flags that take one (e.g. -C <path>)
			if idx < len(args) && !strings.HasPrefix(args[idx], "-") {
				idx++
			}
			continue
		}
		break
	}
	if idx >= len(args) {
		return false, ""
	}

	subcommand := args[idx]

	// Check direct blocklist (checkout, switch)
	if msg, ok := blockedGitSubcommands[subcommand]; ok {
		return true, fmt.Sprintf("BLOCKED: git %s — %s", subcommand, msg)
	}

	// Check worktree subcommands
	if subcommand == "worktree" && idx+1 < len(args) {
		action := args[idx+1]
		if msg, ok := blockedWorktreeActions[action]; ok {
			return true, fmt.Sprintf("BLOCKED: git worktree %s — %s", action, msg)
		}
		// "git worktree list" and "git worktree repair" are allowed
	}

	return false, ""
}

// FilterWorktreeListOutput filters the output of `git worktree list` to show
// only the line matching assignedWorktree.
func FilterWorktreeListOutput(output, assignedWorktree string) string {
	var filtered []string
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, assignedWorktree) {
			filtered = append(filtered, line)
		}
	}
	if len(filtered) == 0 {
		return output // fallback: return unfiltered if no match
	}
	return strings.Join(filtered, "\n") + "\n"
}

// generateGitWrapperScript returns a shell script that wraps git and enforces
// worktree isolation for sub-agents. branchName is the assigned branch and
// worktreePath is the absolute path to the worktree.
func generateGitWrapperScript(branchName, worktreePath string) string {
	return fmt.Sprintf(`#!/usr/bin/env bash
# Flock git wrapper — enforces worktree isolation for sub-agents.
# Branch: %s
# Worktree: %s

set -euo pipefail

REAL_GIT="$(PATH="${PATH#*:}" command -v git)"
ASSIGNED_WORKTREE=%q
ASSIGNED_BRANCH=%q

# Find the first non-flag argument (the git subcommand).
args=("$@")
subcmd=""
subcmd_idx=-1
i=0
while [ $i -lt ${#args[@]} ]; do
    arg="${args[$i]}"
    case "$arg" in
        -*) ((i++)) || true
            # skip value of flags like -C <path>
            if [ $i -lt ${#args[@]} ] && [[ ! "${args[$i]}" =~ ^- ]]; then
                ((i++)) || true
            fi
            ;;
        *)
            subcmd="$arg"
            subcmd_idx=$i
            break
            ;;
    esac
done

case "$subcmd" in
    checkout|switch)
        echo "BLOCKED: git $subcmd — Branch switching is not allowed. Your branch ($ASSIGNED_BRANCH) is fixed to this worktree." >&2
        exit 1
        ;;
    worktree)
        action="${args[$((subcmd_idx+1))]:-}"
        case "$action" in
            add|remove|move|prune)
                echo "BLOCKED: git worktree $action — Worktree management is handled by flock. Do not manage worktrees manually." >&2
                exit 1
                ;;
            list)
                # Run the real command and filter output to show only the assigned worktree
                output=$("$REAL_GIT" "$@" 2>&1) || true
                echo "$output" | grep -F "$ASSIGNED_WORKTREE" || echo "$output"
                exit 0
                ;;
            *)
                exec "$REAL_GIT" "$@"
                ;;
        esac
        ;;
    *)
        exec "$REAL_GIT" "$@"
        ;;
esac
`, branchName, worktreePath, worktreePath, branchName)
}

// installGitFilter writes the git wrapper script and CLAUDE.md into the
// worktree so that sub-agents use the filtered git.
func installGitFilter(worktreePath, branchName string) error {
	// Create .flock/bin/ inside the worktree
	binDir := filepath.Join(worktreePath, ".flock", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("mkdir .flock/bin: %w", err)
	}

	// Write the git wrapper script
	wrapperPath := filepath.Join(binDir, "git")
	script := generateGitWrapperScript(branchName, worktreePath)
	if err := os.WriteFile(wrapperPath, []byte(script), 0o755); err != nil {
		return fmt.Errorf("write git wrapper: %w", err)
	}

	// Write CLAUDE.md with worktree context and PATH instructions
	claudeMD := fmt.Sprintf(`# Flock Worktree Environment

## CRITICAL: Git Command Restrictions

You are working in a flock-managed worktree. The following restrictions are enforced:

- **DO NOT** run `+"`git checkout`"+` or `+"`git switch`"+` — your branch (%s) is fixed.
- **DO NOT** run `+"`git worktree add/remove/move/prune`"+` — flock manages worktrees.
- All other git commands (add, commit, push, status, diff, log, etc.) are allowed.

## Environment Setup

Before running any git command, ensure the flock git wrapper is on your PATH:

`+"`"+`
export PATH="%s/.flock/bin:$PATH"
`+"`"+`

Or prefix individual commands:

`+"`"+`
PATH="%s/.flock/bin:$PATH" git status
`+"`"+`

This wrapper enforces worktree isolation and prevents accidental branch switches or worktree modifications.

## Worktree Details
- **Path**: %s
- **Branch**: %s
- **Do NOT** remove this worktree — flock manages cleanup.
`, branchName, worktreePath, worktreePath, worktreePath, branchName)

	claudePath := filepath.Join(worktreePath, "CLAUDE.md")
	if err := os.WriteFile(claudePath, []byte(claudeMD), 0o644); err != nil {
		return fmt.Errorf("write CLAUDE.md: %w", err)
	}

	// Add .flock/ to .gitignore if not already present
	gitignorePath := filepath.Join(worktreePath, ".gitignore")
	gitignoreContent, _ := os.ReadFile(gitignorePath)
	if !strings.Contains(string(gitignoreContent), ".flock/") {
		f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			defer f.Close()
			if len(gitignoreContent) > 0 && !strings.HasSuffix(string(gitignoreContent), "\n") {
				f.WriteString("\n")
			}
			f.WriteString(".flock/\n")
		}
	}

	return nil
}

// runGitInWorktree executes a git command in a worktree, applying filtering.
// This is for server-side use when Flock itself needs to run git commands on
// behalf of an agent session.
func runGitInWorktree(worktreePath string, args ...string) (string, error) {
	blocked, msg := IsBlockedGitCommand(args)
	if blocked {
		return "", fmt.Errorf("%s", msg)
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = worktreePath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, string(output))
	}

	// Filter worktree list output
	if len(args) >= 2 && args[0] == "worktree" && args[1] == "list" {
		return FilterWorktreeListOutput(string(output), worktreePath), nil
	}

	return string(output), nil
}
