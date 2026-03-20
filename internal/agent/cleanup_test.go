package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestHasUncommittedChanges_Clean(t *testing.T) {
	dir := t.TempDir()
	// Init a git repo
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "test")
	run(t, dir, "git", "config", "commit.gpgsign", "false")
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0o644)
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "initial")

	has, err := hasUncommittedChanges(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Error("expected no uncommitted changes in clean repo")
	}
}

func TestHasUncommittedChanges_Dirty(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "test")
	run(t, dir, "git", "config", "commit.gpgsign", "false")
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0o644)
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "initial")

	// Make a dirty change
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("modified"), 0o644)

	has, err := hasUncommittedChanges(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Error("expected uncommitted changes in dirty repo")
	}
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}
