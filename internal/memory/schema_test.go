package memory

import "testing"

func TestValidateMemoryFile_Valid(t *testing.T) {
	content := `---
date: 2024-01-01
status: accepted
---

# Use SQLite for persistence

## Context
We need a database for state.

## Decision
Use SQLite via modernc.org/sqlite.

## Consequences
No CGO dependency, but limited concurrency.
`
	result := ValidateMemoryFile(content, "technical-decisions")
	if !result.Valid {
		t.Errorf("expected valid, got errors: %v", result.Errors)
	}
}

func TestValidateMemoryFile_MissingFrontmatter(t *testing.T) {
	content := `# No frontmatter

## Context
Some context.
`
	result := ValidateMemoryFile(content, "technical-decisions")
	if result.Valid {
		t.Error("expected invalid for missing frontmatter")
	}
	if len(result.Errors) == 0 {
		t.Error("expected errors")
	}
}

func TestValidateMemoryFile_MissingRequiredField(t *testing.T) {
	content := `---
author: test
---

# Title

## Context
context

## Decision
decision

## Consequences
consequences
`
	result := ValidateMemoryFile(content, "technical-decisions")
	if result.Valid {
		t.Error("expected invalid for missing date and status fields")
	}

	foundDate := false
	foundStatus := false
	for _, e := range result.Errors {
		if e.Field == "frontmatter.date" {
			foundDate = true
		}
		if e.Field == "frontmatter.status" {
			foundStatus = true
		}
	}
	if !foundDate {
		t.Error("expected error for missing date field")
	}
	if !foundStatus {
		t.Error("expected error for missing status field")
	}
}

func TestValidateMemoryFile_MissingHeading(t *testing.T) {
	content := `---
date: 2024-01-01
status: accepted
---

# Title

## Context
context here
`
	result := ValidateMemoryFile(content, "technical-decisions")
	if result.Valid {
		t.Error("expected invalid for missing Decision and Consequences headings")
	}

	foundDecision := false
	foundConsequences := false
	for _, e := range result.Errors {
		if e.Field == "heading.Decision" {
			foundDecision = true
		}
		if e.Field == "heading.Consequences" {
			foundConsequences = true
		}
	}
	if !foundDecision {
		t.Error("expected error for missing Decision heading")
	}
	if !foundConsequences {
		t.Error("expected error for missing Consequences heading")
	}
}

func TestValidateMemoryFile_UnknownCategory(t *testing.T) {
	result := ValidateMemoryFile("content", "nonexistent")
	if result.Valid {
		t.Error("expected invalid for unknown category")
	}
}

func TestValidateMemoryFile_Learnings(t *testing.T) {
	content := `---
date: 2024-03-01
---

# Git worktree corruption

## Problem
Worktrees occasionally lose their .git file.

## Root Cause
Race condition during concurrent writes.

## Lesson
Always validate worktree integrity before use.
`
	result := ValidateMemoryFile(content, "learnings")
	if !result.Valid {
		t.Errorf("expected valid learnings, got errors: %v", result.Errors)
	}
}

func TestExtractHeadings(t *testing.T) {
	content := `# Title
## Context
Some text
## Decision
More text
### Subsection
## Consequences
`
	headings := extractHeadings(content)
	expected := []string{"Context", "Decision", "Consequences"}
	if len(headings) != len(expected) {
		t.Fatalf("expected %d headings, got %d: %v", len(expected), len(headings), headings)
	}
	for i, h := range headings {
		if h != expected[i] {
			t.Errorf("heading[%d] = %q, want %q", i, h, expected[i])
		}
	}
}

func TestParseFrontmatter(t *testing.T) {
	content := `---
date: 2024-01-01
status: accepted
author: test-agent
---

# Content here
`
	fm, errors := parseFrontmatter(content)
	if len(errors) > 0 {
		t.Errorf("unexpected errors: %v", errors)
	}
	if fm["date"] != "2024-01-01" {
		t.Errorf("date = %q, want 2024-01-01", fm["date"])
	}
	if fm["status"] != "accepted" {
		t.Errorf("status = %q, want accepted", fm["status"])
	}
	if fm["author"] != "test-agent" {
		t.Errorf("author = %q, want test-agent", fm["author"])
	}
}

func TestGetCategoryTemplate(t *testing.T) {
	for name := range Categories {
		tmpl, err := GetCategoryTemplate(name)
		if err != nil {
			t.Errorf("GetCategoryTemplate(%q) error: %v", name, err)
		}
		if tmpl == "" {
			t.Errorf("GetCategoryTemplate(%q) returned empty template", name)
		}
	}

	_, err := GetCategoryTemplate("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent category")
	}
}

func TestListCategories(t *testing.T) {
	cats := ListCategories()
	if len(cats) != 5 {
		t.Errorf("expected 5 categories, got %d", len(cats))
	}
}
