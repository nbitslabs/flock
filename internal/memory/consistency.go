package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ConsistencyIssue represents a problem found during consistency validation.
type ConsistencyIssue struct {
	Severity string // "error", "warning", "info"
	FilePath string
	Field    string
	Message  string
}

// ConsistencyReport contains the results of a consistency check.
type ConsistencyReport struct {
	Issues    []ConsistencyIssue
	FilesChecked int
	CheckedAt    time.Time
}

// ConsistencyChecker validates memory files for consistency.
type ConsistencyChecker struct {
	dataDir   string
	repoPath  string // path to the actual git repo for file existence checks
}

// NewConsistencyChecker creates a new ConsistencyChecker.
func NewConsistencyChecker(dataDir, repoPath string) *ConsistencyChecker {
	return &ConsistencyChecker{
		dataDir:  dataDir,
		repoPath: repoPath,
	}
}

// fileRefPattern matches file path references like `src/main.go` or `internal/agent/foo.go`
var fileRefPattern = regexp.MustCompile("`([a-zA-Z0-9_./\\-]+\\.[a-zA-Z0-9]+)`")

// issueRefPattern matches issue references like #42
var issueRefPattern = regexp.MustCompile(`#(\d+)`)

// RunConsistencyCheck validates all memory files in the state directory.
func (cc *ConsistencyChecker) RunConsistencyCheck(org, repo string) ConsistencyReport {
	report := ConsistencyReport{
		CheckedAt: time.Now(),
	}

	repoStatePath := RepoStatePath(cc.dataDir, org, repo)

	filepath.Walk(repoStatePath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") || info.Name() == "HEARTBEAT.md" {
			return nil
		}

		report.FilesChecked++
		issues := cc.checkFile(path)
		report.Issues = append(report.Issues, issues...)
		return nil
	})

	return report
}

// checkFile validates a single memory file for consistency.
func (cc *ConsistencyChecker) checkFile(filePath string) []ConsistencyIssue {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return []ConsistencyIssue{{
			Severity: "error",
			FilePath: filePath,
			Field:    "file",
			Message:  fmt.Sprintf("cannot read file: %v", err),
		}}
	}

	contentStr := string(content)
	var issues []ConsistencyIssue

	// Check frontmatter
	fm, fmErrors := ParseFrontmatter(contentStr)
	if len(fmErrors) > 0 {
		for _, e := range fmErrors {
			issues = append(issues, ConsistencyIssue{
				Severity: "warning",
				FilePath: filePath,
				Field:    e.Field,
				Message:  e.Message,
			})
		}
	}

	// Check date format if present
	if dateStr, ok := fm["date"]; ok {
		if _, err := time.Parse("2006-01-02", dateStr); err != nil {
			issues = append(issues, ConsistencyIssue{
				Severity: "warning",
				FilePath: filePath,
				Field:    "frontmatter.date",
				Message:  fmt.Sprintf("invalid date format %q (expected YYYY-MM-DD)", dateStr),
			})
		}
	}

	// Check file path references exist in the repo
	if cc.repoPath != "" {
		refs := fileRefPattern.FindAllStringSubmatch(contentStr, -1)
		for _, ref := range refs {
			if len(ref) < 2 {
				continue
			}
			refPath := ref[1]
			// Only check paths that look like source files
			if !looksLikeSourcePath(refPath) {
				continue
			}
			fullPath := filepath.Join(cc.repoPath, refPath)
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				issues = append(issues, ConsistencyIssue{
					Severity: "warning",
					FilePath: filePath,
					Field:    "reference",
					Message:  fmt.Sprintf("referenced file %q does not exist in repo", refPath),
				})
			}
		}
	}

	// Check for stale content (file not modified in 90+ days)
	info, err := os.Stat(filePath)
	if err == nil {
		daysSinceModified := time.Since(info.ModTime()).Hours() / 24
		if daysSinceModified > 90 {
			issues = append(issues, ConsistencyIssue{
				Severity: "info",
				FilePath: filePath,
				Field:    "staleness",
				Message:  fmt.Sprintf("file has not been modified in %.0f days, may be stale", daysSinceModified),
			})
		}
	}

	return issues
}

// looksLikeSourcePath returns true if a path looks like a source code file reference.
func looksLikeSourcePath(path string) bool {
	// Must contain a directory separator and a common extension
	if !strings.Contains(path, "/") {
		return false
	}
	exts := []string{".go", ".js", ".ts", ".py", ".rs", ".java", ".rb", ".c", ".h", ".cpp", ".sql", ".yaml", ".yml", ".toml", ".json"}
	for _, ext := range exts {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}
