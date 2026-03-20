package testing

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLearnPatterns_Go(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main_test.go"), []byte(`package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestExample(t *testing.T) {
	assert.Equal(t, 1, 1)
	require.NotNil(t, "hello")
}
`), 0o644)

	config := DiscoverTests(dir)
	if len(config.Frameworks) == 0 {
		t.Fatal("expected at least 1 framework")
	}

	patterns := LearnPatterns(dir, config)
	if len(patterns.Patterns) == 0 {
		t.Fatal("expected at least 1 pattern")
	}

	p := patterns.Patterns[0]
	if p.Language != "go" {
		t.Errorf("expected go, got %s", p.Language)
	}
	if p.NamingConvention == "" {
		t.Error("expected non-empty naming convention")
	}
	if p.AssertionStyle != "testify (assert/require)" {
		t.Errorf("expected testify assertion style, got %s", p.AssertionStyle)
	}
	if p.SetupPattern != "TestMain" {
		t.Errorf("expected TestMain setup pattern, got %s", p.SetupPattern)
	}
	if len(p.ExampleFiles) == 0 {
		t.Error("expected example files")
	}
	if len(p.ExampleContent) == 0 {
		t.Error("expected example content")
	}
}

func TestLearnPatterns_JavaScript(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{
		"scripts": {"test": "jest"},
		"devDependencies": {"jest": "^29.0.0"}
	}`
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644)
	os.WriteFile(filepath.Join(dir, "app.test.js"), []byte(`
const { expect } = require('chai');

describe('App', () => {
	beforeEach(() => {
		// setup
	});

	it('should work', () => {
		expect(true).to.be.true;
	});
});
`), 0o644)

	config := DiscoverTests(dir)
	patterns := LearnPatterns(dir, config)

	if len(patterns.Patterns) == 0 {
		t.Fatal("expected at least 1 pattern")
	}
	p := patterns.Patterns[0]
	if p.Language != "javascript" {
		t.Errorf("expected javascript, got %s", p.Language)
	}
	if p.AssertionStyle != "chai (expect)" {
		t.Errorf("expected chai assertion style, got %s", p.AssertionStyle)
	}
	if p.SetupPattern != "beforeEach/afterEach" {
		t.Errorf("expected beforeEach/afterEach setup pattern, got %s", p.SetupPattern)
	}
}

func TestLearnPatterns_Python(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pytest.ini"), []byte("[pytest]\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "tests"), 0o755)
	os.WriteFile(filepath.Join(dir, "tests", "test_example.py"), []byte(`
import pytest

@pytest.fixture
def client():
    return create_client()

def test_example(client):
    assert client is not None
`), 0o644)

	config := DiscoverTests(dir)
	patterns := LearnPatterns(dir, config)

	if len(patterns.Patterns) == 0 {
		t.Fatal("expected at least 1 pattern")
	}
	p := patterns.Patterns[0]
	if p.Language != "python" {
		t.Errorf("expected python, got %s", p.Language)
	}
	if p.AssertionStyle != "pytest (assert statements)" {
		t.Errorf("expected pytest assertion style, got %s", p.AssertionStyle)
	}
	if p.SetupPattern != "pytest fixtures" {
		t.Errorf("expected pytest fixtures setup pattern, got %s", p.SetupPattern)
	}
}

func TestLearnPatterns_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	config := DiscoverTests(dir)
	patterns := LearnPatterns(dir, config)

	if len(patterns.Patterns) != 0 {
		t.Errorf("expected 0 patterns, got %d", len(patterns.Patterns))
	}
}

func TestIsTestFile(t *testing.T) {
	fw := TestFramework{
		FileGlobs: []string{"*_test.go"},
	}

	if !isTestFile("/path/to/foo_test.go", fw) {
		t.Error("expected foo_test.go to match")
	}
	if isTestFile("/path/to/foo.go", fw) {
		t.Error("expected foo.go to not match")
	}
}

func TestDetectNamingConvention(t *testing.T) {
	tests := []struct {
		lang     string
		files    []string
		expected string
	}{
		{"go", []string{"foo_test.go"}, "file_test.go (TestFunctionName pattern)"},
		{"python", []string{"test_foo.py"}, "test_*.py or *_test.py (def test_function pattern)"},
		{"rust", []string{"lib.rs"}, "#[cfg(test)] mod tests (fn test_name pattern)"},
		{"java", []string{"FooTest.java"}, "*Test.java (@Test annotation pattern)"},
		{"unknown", []string{"test.txt"}, "unknown"},
	}

	for _, tc := range tests {
		fw := TestFramework{Language: tc.lang}
		got := detectNamingConvention(tc.files, fw)
		if got != tc.expected {
			t.Errorf("lang=%s: expected %q, got %q", tc.lang, tc.expected, got)
		}
	}
}

func TestDetectNamingConvention_JSSpec(t *testing.T) {
	fw := TestFramework{Language: "javascript"}
	files := []string{"app.spec.js", "util.spec.js", "helper.test.js"}
	got := detectNamingConvention(files, fw)
	if got != "*.spec.{js,ts} (describe/it pattern)" {
		t.Errorf("expected spec pattern, got %s", got)
	}
}
