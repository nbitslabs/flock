package memory

import (
	"fmt"
	"strings"
)

// MemoryCategory defines a category of memory with required structure.
type MemoryCategory struct {
	Name             string
	Description      string
	RequiredHeadings []string
	RequiredFields   []string // frontmatter fields
	OptionalFields   []string
	Template         string
}

// Categories defines the 5 supported memory categories with their schemas.
var Categories = map[string]MemoryCategory{
	"technical-decisions": {
		Name:             "technical-decisions",
		Description:      "Records of technical decisions with context and consequences",
		RequiredHeadings: []string{"Context", "Decision", "Consequences"},
		RequiredFields:   []string{"date", "status"},
		OptionalFields:   []string{"author", "tags", "supersedes"},
		Template: `---
date: 2024-01-01
status: accepted
author: agent
tags: []
---

# Title

## Context
What is the issue or background?

## Decision
What was decided?

## Consequences
What are the trade-offs and implications?
`,
	},
	"patterns": {
		Name:             "patterns",
		Description:      "Reusable code patterns and conventions observed in the project",
		RequiredHeadings: []string{"Pattern", "Usage"},
		RequiredFields:   []string{"date", "category"},
		OptionalFields:   []string{"author", "tags", "language"},
		Template: `---
date: 2024-01-01
category: code-style
author: agent
tags: []
---

# Title

## Pattern
Describe the pattern and when to use it.

## Usage
Show example usage with code blocks.
`,
	},
	"learnings": {
		Name:             "learnings",
		Description:      "Lessons learned from debugging, incidents, or development",
		RequiredHeadings: []string{"Problem", "Root Cause", "Lesson"},
		RequiredFields:   []string{"date"},
		OptionalFields:   []string{"author", "tags", "issue_number", "severity"},
		Template: `---
date: 2024-01-01
author: agent
tags: []
---

# Title

## Problem
What went wrong or was discovered?

## Root Cause
What was the underlying cause?

## Lesson
What should be remembered for the future?
`,
	},
	"dependencies": {
		Name:             "dependencies",
		Description:      "External dependencies, APIs, and integration details",
		RequiredHeadings: []string{"Overview", "Configuration"},
		RequiredFields:   []string{"date", "name"},
		OptionalFields:   []string{"author", "tags", "version", "url"},
		Template: `---
date: 2024-01-01
name: dependency-name
version: "1.0"
author: agent
tags: []
---

# Title

## Overview
What is this dependency and why is it used?

## Configuration
How is it configured in this project?
`,
	},
	"architecture": {
		Name:             "architecture",
		Description:      "System architecture descriptions, component relationships, and data flows",
		RequiredHeadings: []string{"Overview", "Components"},
		RequiredFields:   []string{"date"},
		OptionalFields:   []string{"author", "tags", "scope"},
		Template: `---
date: 2024-01-01
author: agent
tags: []
scope: system
---

# Title

## Overview
High-level description of the architecture.

## Components
List and describe the key components.
`,
	},
}

// ValidationError represents a specific validation failure.
type ValidationError struct {
	Field   string // e.g. "frontmatter.date" or "heading.Decision"
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationResult contains the result of schema validation.
type ValidationResult struct {
	Valid    bool
	Errors  []ValidationError
	Warnings []ValidationError
}

// Frontmatter represents parsed YAML frontmatter from a markdown file.
type Frontmatter map[string]string

// ValidateMemoryFile validates a markdown file against its category schema.
func ValidateMemoryFile(content, category string) ValidationResult {
	result := ValidationResult{Valid: true}

	cat, ok := Categories[category]
	if !ok {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "category",
			Message: fmt.Sprintf("unknown category %q; valid categories: %s", category, validCategoryNames()),
		})
		return result
	}

	// Parse frontmatter
	fm, fmErrors := ParseFrontmatter(content)
	result.Errors = append(result.Errors, fmErrors...)

	// Validate required frontmatter fields
	for _, field := range cat.RequiredFields {
		if _, ok := fm[field]; !ok {
			result.Errors = append(result.Errors, ValidationError{
				Field:   "frontmatter." + field,
				Message: fmt.Sprintf("required field %q is missing from frontmatter", field),
			})
		}
	}

	// Validate required headings
	headings := extractHeadings(content)
	for _, required := range cat.RequiredHeadings {
		found := false
		for _, h := range headings {
			if strings.EqualFold(h, required) {
				found = true
				break
			}
		}
		if !found {
			result.Errors = append(result.Errors, ValidationError{
				Field:   "heading." + required,
				Message: fmt.Sprintf("required heading \"## %s\" is missing", required),
			})
		}
	}

	if len(result.Errors) > 0 {
		result.Valid = false
	}

	return result
}

// ParseFrontmatter extracts YAML frontmatter from markdown content.
// Returns field map and any parse errors.
func ParseFrontmatter(content string) (Frontmatter, []ValidationError) {
	fm := make(Frontmatter)
	var errors []ValidationError

	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		errors = append(errors, ValidationError{
			Field:   "frontmatter",
			Message: "missing frontmatter block (must start with ---)",
		})
		return fm, errors
	}

	// Find closing ---
	rest := content[3:]
	endIdx := strings.Index(rest, "\n---")
	if endIdx == -1 {
		errors = append(errors, ValidationError{
			Field:   "frontmatter",
			Message: "unclosed frontmatter block (missing closing ---)",
		})
		return fm, errors
	}

	block := rest[:endIdx]
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			fm[key] = value
		}
	}

	return fm, errors
}

// extractHeadings returns all ## level headings from markdown content.
func extractHeadings(content string) []string {
	var headings []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			heading := strings.TrimPrefix(trimmed, "## ")
			headings = append(headings, strings.TrimSpace(heading))
		}
	}
	return headings
}

// validCategoryNames returns a comma-separated list of valid category names.
func validCategoryNames() string {
	names := make([]string, 0, len(Categories))
	for name := range Categories {
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

// GetCategoryTemplate returns the template for a given category.
func GetCategoryTemplate(category string) (string, error) {
	cat, ok := Categories[category]
	if !ok {
		return "", fmt.Errorf("unknown category %q", category)
	}
	return cat.Template, nil
}

// ListCategories returns all available category names.
func ListCategories() []string {
	names := make([]string, 0, len(Categories))
	for name := range Categories {
		names = append(names, name)
	}
	return names
}
