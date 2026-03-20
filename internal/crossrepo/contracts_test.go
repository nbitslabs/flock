package crossrepo

import (
	"strings"
	"testing"
)

func TestDetectContractChanges(t *testing.T) {
	files := []string{
		"api/openapi.yaml",
		"src/main.go",
		"schema.graphql",
		"proto/service.proto",
		"types/api.d.ts",
		"README.md",
	}

	changes := DetectContractChanges(files)
	if len(changes) != 4 {
		t.Errorf("expected 4 contract changes, got %d", len(changes))
		for _, c := range changes {
			t.Logf("  %s (%s)", c.FilePath, c.ContractType)
		}
	}
}

func TestIdentifyContractType(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"api/openapi.yaml", "openapi"},
		{"swagger.json", "openapi"},
		{"schema.graphql", "graphql"},
		{"service.proto", "proto"},
		{"types.d.ts", "typescript"},
		{"main.go", ""},
		{"README.md", ""},
	}

	for _, tc := range tests {
		got := identifyContractType(tc.path)
		if got != tc.expected {
			t.Errorf("identifyContractType(%q) = %q, want %q", tc.path, got, tc.expected)
		}
	}
}

func TestIsBreakingChange(t *testing.T) {
	old := "paths:\n" + strings.Repeat("x", 100)
	short := "paths:\n" + strings.Repeat("x", 50) // ~50% of original

	if !IsBreakingChange("openapi", old, short) {
		t.Error("expected breaking change for significant shrinkage")
	}

	if IsBreakingChange("openapi", old, old) {
		t.Error("expected non-breaking for same size")
	}

	// GraphQL breaking change
	oldGql := strings.Repeat("type Query { }", 10)
	shortGql := strings.Repeat("type Query { }", 5)
	if !IsBreakingChange("graphql", oldGql, shortGql) {
		t.Error("expected breaking change for graphql shrinkage")
	}
}

func TestBuildPRAnnotation(t *testing.T) {
	result := &ContractValidationResult{
		Provider: "org/backend",
		Contract: ContractChange{
			FilePath:     "api/openapi.yaml",
			ContractType: "openapi",
		},
		AllPassed: true,
		Results: []ConsumerResult{
			{Consumer: "org/frontend", Passed: true},
		},
	}

	annotation := BuildPRAnnotation(result)
	if !strings.Contains(annotation, "Passed") {
		t.Error("expected 'Passed' in annotation")
	}
	if !strings.Contains(annotation, "org/backend") {
		t.Error("expected provider in annotation")
	}
}
