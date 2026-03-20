package testing

import (
	"fmt"
	"regexp"
	"strings"
)

// FailureDetail contains parsed information from a test failure.
type FailureDetail struct {
	TestName      string `json:"test_name"`
	FilePath      string `json:"file_path,omitempty"`
	Line          int    `json:"line,omitempty"`
	AssertionText string `json:"assertion_text,omitempty"`
	StackTrace    string `json:"stack_trace,omitempty"`
	InputValues   string `json:"input_values,omitempty"`
	ErrorMessage  string `json:"error_message"`
}

// RootCause represents a suggested root cause for a test failure.
type RootCause struct {
	Description string  `json:"description"`
	Confidence  float64 `json:"confidence"` // 0.0 to 1.0
	Category    string  `json:"category"`   // e.g., "assertion", "nil_pointer", "timeout", "compilation"
	Suggestion  string  `json:"suggestion,omitempty"`
}

// HistoricalMatch represents a previous failure that matches the current one.
type HistoricalMatch struct {
	FailureID      string  `json:"failure_id"`
	TestName       string  `json:"test_name"`
	Similarity     float64 `json:"similarity"`
	WasResolved    bool    `json:"was_resolved"`
	FixDescription string  `json:"fix_description,omitempty"`
}

// AnalysisResult contains the full analysis of a test failure.
type AnalysisResult struct {
	Failure           FailureDetail     `json:"failure"`
	RootCauses        []RootCause       `json:"root_causes"`
	HistoricalMatches []HistoricalMatch `json:"historical_matches,omitempty"`
	Context           string            `json:"context,omitempty"`
}

// Go failure patterns
var (
	goFailLinePattern    = regexp.MustCompile(`--- FAIL: (\w+(?:/\w+)*)\s+\([\d.]+s\)`)
	goFileLinePattern    = regexp.MustCompile(`(\S+\.go):(\d+):\s+(.+)`)
	goExpectedGotPattern = regexp.MustCompile(`expected\s+(.+),\s+got\s+(.+)`)
	goPanicPattern       = regexp.MustCompile(`panic:\s+(.+)`)
	goNilPointerPattern  = regexp.MustCompile(`runtime error: invalid memory address or nil pointer dereference`)
	goTimeoutPattern     = regexp.MustCompile(`(?i)test timed out|context deadline exceeded|context canceled`)
	goBuildFailPattern   = regexp.MustCompile(`(?m)^# .+\n.+\.go:\d+:\d+:`)
)

// JS/TS failure patterns
var (
	jestFailPattern     = regexp.MustCompile(`● (.+)`)
	jestExpectPattern   = regexp.MustCompile(`Expected:?\s*(.+)\n\s*Received:?\s*(.+)`)
	jestFileLinePattern = regexp.MustCompile(`at .+ \((.+):(\d+):\d+\)`)
)

// Python failure patterns
var (
	pytestFailPattern   = regexp.MustCompile(`FAILED (.+)::(.+)`)
	pytestAssertPattern = regexp.MustCompile(`(?m)>\s+assert\s+(.+)\nE\s+(.+)`)
)

// AnalyzeFailure parses test output and extracts structured failure details.
func AnalyzeFailure(framework, output string) []FailureDetail {
	switch framework {
	case "go-test":
		return analyzeGoFailure(output)
	case "jest", "vitest":
		return analyzeJSFailure(output)
	case "pytest":
		return analyzePythonFailure(output)
	default:
		return analyzeGenericFailure(output)
	}
}

func analyzeGoFailure(output string) []FailureDetail {
	var details []FailureDetail
	lines := strings.Split(output, "\n")

	failMatches := goFailLinePattern.FindAllStringSubmatch(output, -1)
	for _, m := range failMatches {
		detail := FailureDetail{
			TestName: m[1],
		}

		inTest := false
		var stackLines []string
		for _, line := range lines {
			if strings.Contains(line, "--- FAIL: "+m[1]) {
				inTest = true
				continue
			}
			if inTest {
				if strings.HasPrefix(strings.TrimSpace(line), "--- ") || strings.HasPrefix(line, "FAIL") {
					break
				}
				if fileMatch := goFileLinePattern.FindStringSubmatch(line); fileMatch != nil {
					detail.FilePath = fileMatch[1]
					detail.ErrorMessage = fileMatch[3]
				}
				if expMatch := goExpectedGotPattern.FindStringSubmatch(line); expMatch != nil {
					detail.AssertionText = strings.TrimSpace(line)
					detail.InputValues = "expected=" + expMatch[1] + " got=" + expMatch[2]
				}
				stackLines = append(stackLines, strings.TrimSpace(line))
			}
		}

		if len(stackLines) > 0 {
			detail.StackTrace = strings.Join(stackLines, "\n")
		}
		if detail.ErrorMessage == "" && len(stackLines) > 0 {
			detail.ErrorMessage = stackLines[0]
		}

		details = append(details, detail)
	}

	// Check for panics
	if panicMatch := goPanicPattern.FindStringSubmatch(output); panicMatch != nil && len(details) == 0 {
		detail := FailureDetail{
			TestName:     "panic",
			ErrorMessage: "panic: " + panicMatch[1],
		}
		idx := strings.Index(output, "panic:")
		if idx >= 0 {
			trace := output[idx:]
			if len(trace) > 500 {
				trace = trace[:500]
			}
			detail.StackTrace = trace
		}
		details = append(details, detail)
	}

	return details
}

func analyzeJSFailure(output string) []FailureDetail {
	var details []FailureDetail

	failMatches := jestFailPattern.FindAllStringSubmatch(output, -1)
	for _, m := range failMatches {
		detail := FailureDetail{
			TestName:     m[1],
			ErrorMessage: m[1],
		}

		if expMatch := jestExpectPattern.FindStringSubmatch(output); expMatch != nil {
			detail.AssertionText = "Expected: " + strings.TrimSpace(expMatch[1]) + ", Received: " + strings.TrimSpace(expMatch[2])
			detail.InputValues = "expected=" + strings.TrimSpace(expMatch[1]) + " received=" + strings.TrimSpace(expMatch[2])
		}

		if fileMatch := jestFileLinePattern.FindStringSubmatch(output); fileMatch != nil {
			detail.FilePath = fileMatch[1]
		}

		details = append(details, detail)
	}

	return details
}

func analyzePythonFailure(output string) []FailureDetail {
	var details []FailureDetail

	failMatches := pytestFailPattern.FindAllStringSubmatch(output, -1)
	for _, m := range failMatches {
		detail := FailureDetail{
			TestName:     m[2],
			FilePath:     m[1],
			ErrorMessage: "FAILED " + m[1] + "::" + m[2],
		}

		if assertMatch := pytestAssertPattern.FindStringSubmatch(output); assertMatch != nil {
			detail.AssertionText = "assert " + strings.TrimSpace(assertMatch[1])
			detail.ErrorMessage = strings.TrimSpace(assertMatch[2])
		}

		details = append(details, detail)
	}

	return details
}

func analyzeGenericFailure(output string) []FailureDetail {
	lines := strings.Split(output, "\n")

	start := max(len(lines)-20, 0)
	errorLines := lines[start:]

	return []FailureDetail{{
		TestName:     "unknown",
		ErrorMessage: strings.Join(errorLines, "\n"),
	}}
}

// SuggestRootCauses analyzes a failure and suggests likely root causes.
func SuggestRootCauses(framework, output string) []RootCause {
	var causes []RootCause

	if goNilPointerPattern.MatchString(output) {
		causes = append(causes, RootCause{
			Description: "Nil pointer dereference - a variable was used before initialization or after being set to nil",
			Confidence:  0.95,
			Category:    "nil_pointer",
			Suggestion:  "Check that all pointers and interface values are properly initialized before use. Look for missing nil checks.",
		})
	}

	if goPanicPattern.MatchString(output) && !goNilPointerPattern.MatchString(output) {
		panicMatch := goPanicPattern.FindStringSubmatch(output)
		causes = append(causes, RootCause{
			Description: "Runtime panic: " + panicMatch[1],
			Confidence:  0.9,
			Category:    "panic",
			Suggestion:  "Review the panic message and stack trace. Common causes: index out of bounds, nil pointer, type assertion failure.",
		})
	}

	if goTimeoutPattern.MatchString(output) {
		causes = append(causes, RootCause{
			Description: "Test timed out or context was cancelled",
			Confidence:  0.9,
			Category:    "timeout",
			Suggestion:  "Check for infinite loops, blocking operations without timeouts, or slow external dependencies.",
		})
	}

	if goBuildFailPattern.MatchString(output) {
		causes = append(causes, RootCause{
			Description: "Compilation error - code does not build",
			Confidence:  0.95,
			Category:    "compilation",
			Suggestion:  "Fix the compilation errors before running tests. Check for missing imports, type mismatches, or undefined references.",
		})
	}

	if goExpectedGotPattern.MatchString(output) {
		match := goExpectedGotPattern.FindStringSubmatch(output)
		causes = append(causes, RootCause{
			Description: "Assertion failure: expected " + match[1] + " but got " + match[2],
			Confidence:  0.85,
			Category:    "assertion",
			Suggestion:  "The function returned an unexpected value. Check the logic in the function under test and verify the test expectations are correct.",
		})
	}

	if strings.Contains(output, "connection refused") || strings.Contains(output, "no such host") {
		causes = append(causes, RootCause{
			Description: "Network/connection error - a required service may not be running",
			Confidence:  0.85,
			Category:    "network",
			Suggestion:  "Ensure all required services (databases, APIs) are running. Check if the test needs environment setup.",
		})
	}

	if strings.Contains(output, "permission denied") || strings.Contains(output, "access denied") {
		causes = append(causes, RootCause{
			Description: "Permission denied - insufficient access rights",
			Confidence:  0.85,
			Category:    "permission",
			Suggestion:  "Check file permissions, environment variables, and service credentials.",
		})
	}

	if len(causes) == 0 {
		causes = append(causes, RootCause{
			Description: "Test assertion failed - review the test output for specific details",
			Confidence:  0.5,
			Category:    "unknown",
			Suggestion:  "Examine the test output and compare expected vs actual values. Check recent code changes that may have affected behavior.",
		})
	}

	return causes
}

// CompareFailures computes a similarity score between two failure details.
// Returns a value between 0.0 (completely different) and 1.0 (identical).
func CompareFailures(a, b FailureDetail) float64 {
	var score float64
	var weights float64

	if a.TestName == b.TestName {
		score += 3.0
	} else if strings.Contains(a.TestName, b.TestName) || strings.Contains(b.TestName, a.TestName) {
		score += 1.5
	}
	weights += 3.0

	if a.FilePath != "" && b.FilePath != "" {
		if a.FilePath == b.FilePath {
			score += 2.0
		}
	}
	weights += 2.0

	if a.ErrorMessage != "" && b.ErrorMessage != "" {
		errSim := stringSimilarity(a.ErrorMessage, b.ErrorMessage)
		score += errSim * 2.0
	}
	weights += 2.0

	if a.AssertionText != "" && b.AssertionText != "" {
		assertSim := stringSimilarity(a.AssertionText, b.AssertionText)
		score += assertSim * 1.0
	}
	weights += 1.0

	if weights == 0 {
		return 0
	}
	return score / weights
}

// stringSimilarity computes a simple word-overlap similarity between two strings.
func stringSimilarity(a, b string) float64 {
	wordsA := strings.Fields(strings.ToLower(a))
	wordsB := strings.Fields(strings.ToLower(b))

	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0
	}

	setB := make(map[string]bool, len(wordsB))
	for _, w := range wordsB {
		setB[w] = true
	}

	var overlap int
	for _, w := range wordsA {
		if setB[w] {
			overlap++
		}
	}

	union := len(wordsA) + len(wordsB) - overlap
	if union == 0 {
		return 0
	}
	return float64(overlap) / float64(union)
}

// FormatAnalysisForAgent formats analysis results as structured context
// suitable for providing to an AI agent.
func FormatAnalysisForAgent(results []AnalysisResult) string {
	if len(results) == 0 {
		return "No test failures detected."
	}

	var b strings.Builder
	b.WriteString("## Test Failure Analysis\n\n")

	for i, r := range results {
		fmt.Fprintf(&b, "### Failure %d: %s\n\n", i+1, r.Failure.TestName)

		if r.Failure.FilePath != "" {
			fmt.Fprintf(&b, "**File:** `%s`\n", r.Failure.FilePath)
		}

		fmt.Fprintf(&b, "**Error:** %s\n\n", r.Failure.ErrorMessage)

		if r.Failure.AssertionText != "" {
			fmt.Fprintf(&b, "**Assertion:** %s\n\n", r.Failure.AssertionText)
		}

		if len(r.RootCauses) > 0 {
			b.WriteString("**Likely Root Causes:**\n")
			for _, rc := range r.RootCauses {
				fmt.Fprintf(&b, "- %s (confidence: %.0f%%)\n", rc.Description, rc.Confidence*100)
				if rc.Suggestion != "" {
					fmt.Fprintf(&b, "  *Suggestion:* %s\n", rc.Suggestion)
				}
			}
			b.WriteString("\n")
		}

		if len(r.HistoricalMatches) > 0 {
			b.WriteString("**Similar Historical Failures:**\n")
			for _, hm := range r.HistoricalMatches {
				fmt.Fprintf(&b, "- `%s` (similarity: %.0f%%)", hm.TestName, hm.Similarity*100)
				if hm.WasResolved && hm.FixDescription != "" {
					fmt.Fprintf(&b, " — Fixed by: %s", hm.FixDescription)
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}
