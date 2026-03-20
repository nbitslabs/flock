package crossrepo

import (
	"path/filepath"
	"strings"
)

// ContractChange represents a detected change to an API contract file.
type ContractChange struct {
	FilePath     string `json:"file_path"`
	ContractType string `json:"contract_type"` // openapi, graphql, proto, typescript
	IsBreaking   bool   `json:"is_breaking"`
	Description  string `json:"description"`
}

// ContractValidationResult contains the result of validating contracts
// against dependent repositories.
type ContractValidationResult struct {
	Provider     string            `json:"provider"`      // org/repo that changed the contract
	Contract     ContractChange    `json:"contract"`
	Consumers    []string          `json:"consumers"`     // affected repos
	Results      []ConsumerResult  `json:"results"`
	AllPassed    bool              `json:"all_passed"`
}

// ConsumerResult contains the validation result for a single consumer.
type ConsumerResult struct {
	Consumer string `json:"consumer"` // org/repo
	Passed   bool   `json:"passed"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
}

// DetectContractChanges examines changed files and identifies contract modifications.
func DetectContractChanges(changedFiles []string) []ContractChange {
	var changes []ContractChange

	for _, f := range changedFiles {
		ct := identifyContractType(f)
		if ct == "" {
			continue
		}
		changes = append(changes, ContractChange{
			FilePath:     f,
			ContractType: ct,
			Description:  "Modified " + ct + " contract: " + filepath.Base(f),
		})
	}

	return changes
}

// identifyContractType determines if a file is a contract file by its path/extension.
func identifyContractType(path string) string {
	base := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))

	// OpenAPI
	if strings.Contains(base, "openapi") || strings.Contains(base, "swagger") {
		if ext == ".yaml" || ext == ".yml" || ext == ".json" {
			return "openapi"
		}
	}

	// GraphQL
	if ext == ".graphql" || ext == ".gql" {
		return "graphql"
	}
	if strings.Contains(base, "schema") && ext == ".graphql" {
		return "graphql"
	}

	// Protocol Buffers
	if ext == ".proto" {
		return "proto"
	}

	// TypeScript type definitions
	if strings.HasSuffix(base, ".d.ts") {
		return "typescript"
	}

	// JSON Schema
	if strings.Contains(base, "schema") && ext == ".json" {
		return "json-schema"
	}

	return ""
}

// IsBreakingChange performs a basic analysis of whether a contract change
// is likely breaking. For real production use, this would integrate with
// contract-specific diffing tools.
func IsBreakingChange(contractType string, oldContent, newContent string) bool {
	switch contractType {
	case "openapi":
		// Rough heuristic: removed paths or changed methods are breaking
		return strings.Contains(oldContent, "paths:") &&
			len(newContent) < len(oldContent)*8/10
	case "graphql":
		// Removed types or fields are breaking
		return len(newContent) < len(oldContent)*8/10
	case "proto":
		// Any removal in proto is potentially breaking
		return len(newContent) < len(oldContent)*9/10
	default:
		// Conservative: assume breaking if content shrunk significantly
		return len(newContent) < len(oldContent)*7/10
	}
}

// BuildPRAnnotation creates a PR comment annotation for contract validation results.
func BuildPRAnnotation(result *ContractValidationResult) string {
	var b strings.Builder

	if result.AllPassed {
		b.WriteString("## ✅ Contract Validation Passed\n\n")
	} else {
		b.WriteString("## ❌ Contract Validation Failed\n\n")
	}

	b.WriteString("**Contract:** `" + result.Contract.FilePath + "` (" + result.Contract.ContractType + ")\n")
	b.WriteString("**Provider:** " + result.Provider + "\n\n")

	if len(result.Results) > 0 {
		b.WriteString("### Consumer Results\n\n")
		for _, r := range result.Results {
			if r.Passed {
				b.WriteString("- ✅ **" + r.Consumer + "**: Passed\n")
			} else {
				b.WriteString("- ❌ **" + r.Consumer + "**: Failed\n")
				if r.Error != "" {
					b.WriteString("  ```\n  " + r.Error + "\n  ```\n")
				}
			}
		}
	}

	return b.String()
}
