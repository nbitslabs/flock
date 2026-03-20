package testing

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// TestTimeout is the maximum time to wait for tests to complete.
	TestTimeout = 10 * time.Minute

	// CoverageThreshold is the minimum acceptable coverage for changed code.
	CoverageThreshold = 80.0
)

// TestResult contains the result of running tests.
type TestResult struct {
	Framework   string  `json:"framework"`
	Passed      bool    `json:"passed"`
	Output      string  `json:"output"`
	Coverage    float64 `json:"coverage"`
	Duration    string  `json:"duration"`
	TimedOut    bool    `json:"timed_out"`
	ErrorDetail string  `json:"error_detail,omitempty"`
}

// PreCommitCheck runs tests and checks coverage before a commit is allowed.
func PreCommitCheck(ctx context.Context, repoPath string, config TestConfig) []TestResult {
	var results []TestResult

	for _, fw := range config.Frameworks {
		result := runTestsForFramework(ctx, repoPath, fw)
		results = append(results, result)
	}

	return results
}

// AllTestsPassed returns true if all test results indicate success.
func AllTestsPassed(results []TestResult) bool {
	for _, r := range results {
		if !r.Passed {
			return false
		}
	}
	return true
}

// CoverageAboveThreshold returns true if all frameworks meet the coverage threshold.
func CoverageAboveThreshold(results []TestResult) bool {
	for _, r := range results {
		if r.Coverage > 0 && r.Coverage < CoverageThreshold {
			return false
		}
	}
	return true
}

// runTestsForFramework runs tests for a specific framework with coverage.
func runTestsForFramework(ctx context.Context, repoPath string, fw TestFramework) TestResult {
	result := TestResult{
		Framework: fw.Name,
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, TestTimeout)
	defer cancel()

	// Build the command with coverage flags
	cmdStr := addCoverageFlags(fw)

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	result.Duration = time.Since(start).Round(time.Millisecond).String()
	result.Output = string(output)

	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.ErrorDetail = fmt.Sprintf("test execution timed out after %s", TestTimeout)
		return result
	}

	if err != nil {
		result.Passed = false
		result.ErrorDetail = extractFailureDetails(fw.Name, string(output))
	} else {
		result.Passed = true
	}

	// Parse coverage from output
	result.Coverage = parseCoverage(fw.Name, string(output))

	return result
}

// addCoverageFlags adds framework-specific coverage flags to the test command.
func addCoverageFlags(fw TestFramework) string {
	switch fw.Name {
	case "go-test":
		return "go test -cover -count=1 ./..."
	case "jest":
		return "npx jest --coverage --passWithNoTests"
	case "vitest":
		return "npx vitest run --coverage"
	case "pytest":
		return "pytest --cov --cov-report=term -q"
	case "cargo-test":
		return "cargo test"
	case "maven":
		return "mvn test -q"
	case "gradle":
		return "./gradlew test -q"
	default:
		return fw.Command
	}
}

// goCoveragePattern matches Go coverage output like "coverage: 78.5% of statements"
var goCoveragePattern = regexp.MustCompile(`coverage:\s+([\d.]+)%`)

// jestCoveragePattern matches Jest coverage output like "All files | 85.71 |"
var jestCoveragePattern = regexp.MustCompile(`All files\s*\|\s*([\d.]+)`)

// pytestCoveragePattern matches pytest-cov output like "TOTAL  85%"
var pytestCoveragePattern = regexp.MustCompile(`TOTAL\s+\d+\s+\d+\s+(\d+)%`)

// parseCoverage extracts coverage percentage from test output.
func parseCoverage(framework, output string) float64 {
	var pattern *regexp.Regexp

	switch framework {
	case "go-test":
		pattern = goCoveragePattern
	case "jest", "vitest":
		pattern = jestCoveragePattern
	case "pytest":
		pattern = pytestCoveragePattern
	default:
		return 0
	}

	matches := pattern.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return 0
	}

	// For Go, average across all packages
	if framework == "go-test" {
		var total float64
		var count int
		for _, m := range matches {
			if len(m) >= 2 {
				if v, err := strconv.ParseFloat(m[1], 64); err == nil {
					total += v
					count++
				}
			}
		}
		if count > 0 {
			return total / float64(count)
		}
		return 0
	}

	// For other frameworks, take the last (overall) match
	last := matches[len(matches)-1]
	if len(last) >= 2 {
		if v, err := strconv.ParseFloat(last[1], 64); err == nil {
			return v
		}
	}

	return 0
}

// extractFailureDetails extracts the key failure information from test output.
func extractFailureDetails(framework, output string) string {
	lines := strings.Split(output, "\n")
	var failures []string

	switch framework {
	case "go-test":
		for _, line := range lines {
			if strings.Contains(line, "FAIL") || strings.Contains(line, "--- FAIL") {
				failures = append(failures, strings.TrimSpace(line))
			}
		}
	case "jest", "vitest":
		for _, line := range lines {
			if strings.Contains(line, "FAIL") || strings.Contains(line, "●") {
				failures = append(failures, strings.TrimSpace(line))
			}
		}
	case "pytest":
		for _, line := range lines {
			if strings.Contains(line, "FAILED") || strings.Contains(line, "ERROR") {
				failures = append(failures, strings.TrimSpace(line))
			}
		}
	default:
		// Return last 10 lines for unknown frameworks
		start := len(lines) - 10
		if start < 0 {
			start = 0
		}
		failures = lines[start:]
	}

	if len(failures) > 10 {
		failures = failures[:10]
	}

	return strings.Join(failures, "\n")
}
