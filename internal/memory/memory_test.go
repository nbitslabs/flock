package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveStateDir(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "regular directory",
			input:    "/Users/test/project",
			expected: "/Users/test/project/.flock",
		},
		{
			name:     "directory already ending with .flock",
			input:    "/Users/test/project/.flock",
			expected: "/Users/test/project/.flock",
		},
		{
			name:     "current directory",
			input:    ".",
			expected: ".flock",
		},
		{
			name:     "current directory with .flock",
			input:    ".flock",
			expected: ".flock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveStateDir(tt.input)
			if result != tt.expected {
				t.Errorf("ResolveStateDir(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestEnsureLayoutWithFlockSuffix(t *testing.T) {
	tmpDir := t.TempDir()

	flockDir := filepath.Join(tmpDir, ".flock")
	if err := os.MkdirAll(flockDir, 0o755); err != nil {
		t.Fatalf("failed to create .flock dir: %v", err)
	}

	if err := EnsureLayout(tmpDir); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	expectedMemoryDir := filepath.Join(flockDir, "memory")
	if _, err := os.Stat(expectedMemoryDir); os.IsNotExist(err) {
		t.Errorf("expected memory dir at %s, but it doesn't exist", expectedMemoryDir)
	}

	wrongMemoryDir := filepath.Join(flockDir, ".flock", "memory")
	if _, err := os.Stat(wrongMemoryDir); !os.IsNotExist(err) {
		t.Errorf("found nested .flock directory at %s, should not exist", wrongMemoryDir)
	}
}

func TestEnsureLayoutWithoutFlockSuffix(t *testing.T) {
	tmpDir := t.TempDir()

	if err := EnsureLayout(tmpDir); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	expectedMemoryDir := filepath.Join(tmpDir, ".flock", "memory")
	if _, err := os.Stat(expectedMemoryDir); os.IsNotExist(err) {
		t.Errorf("expected memory dir at %s, but it doesn't exist", expectedMemoryDir)
	}
}
