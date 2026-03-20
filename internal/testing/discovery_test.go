package testing

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverTests_Go(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main_test.go"), []byte("package main\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "internal", "foo"), 0o755)
	os.WriteFile(filepath.Join(dir, "internal", "foo", "foo_test.go"), []byte("package foo\n"), 0o644)

	config := DiscoverTests(dir)
	if len(config.Frameworks) != 1 {
		t.Fatalf("expected 1 framework, got %d", len(config.Frameworks))
	}
	fw := config.Frameworks[0]
	if fw.Name != "go-test" {
		t.Errorf("expected go-test, got %s", fw.Name)
	}
	if fw.Command != "go test ./..." {
		t.Errorf("expected 'go test ./...', got %s", fw.Command)
	}
	if len(fw.TestDirs) < 2 {
		t.Errorf("expected at least 2 test dirs, got %d", len(fw.TestDirs))
	}
}

func TestDiscoverTests_NodeJS(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{
		"scripts": {"test": "jest"},
		"devDependencies": {"jest": "^29.0.0"}
	}`
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644)

	config := DiscoverTests(dir)
	if len(config.Frameworks) != 1 {
		t.Fatalf("expected 1 framework, got %d", len(config.Frameworks))
	}
	if config.Frameworks[0].Name != "jest" {
		t.Errorf("expected jest, got %s", config.Frameworks[0].Name)
	}
}

func TestDiscoverTests_Python(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pytest.ini"), []byte("[pytest]\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "tests"), 0o755)

	config := DiscoverTests(dir)
	if len(config.Frameworks) != 1 {
		t.Fatalf("expected 1 framework, got %d", len(config.Frameworks))
	}
	if config.Frameworks[0].Name != "pytest" {
		t.Errorf("expected pytest, got %s", config.Frameworks[0].Name)
	}
}

func TestDiscoverTests_Empty(t *testing.T) {
	dir := t.TempDir()
	config := DiscoverTests(dir)
	if len(config.Frameworks) != 0 {
		t.Errorf("expected 0 frameworks, got %d", len(config.Frameworks))
	}
}
