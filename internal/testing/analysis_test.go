package testing

import (
	"strings"
	"testing"
)

func TestAnalyzeFailure_Go(t *testing.T) {
	output := `--- FAIL: TestSomething (0.05s)
    main_test.go:15: expected true, got false
FAIL	example.com/pkg	0.5s
`
	details := AnalyzeFailure("go-test", output)
	if len(details) == 0 {
		t.Fatal("expected at least 1 failure detail")
	}
	d := details[0]
	if d.TestName != "TestSomething" {
		t.Errorf("expected TestSomething, got %s", d.TestName)
	}
	if d.FilePath != "main_test.go" {
		t.Errorf("expected main_test.go, got %s", d.FilePath)
	}
	if d.ErrorMessage == "" {
		t.Error("expected non-empty error message")
	}
}

func TestAnalyzeFailure_GoPanic(t *testing.T) {
	output := `panic: runtime error: index out of range [5] with length 3

goroutine 1 [running]:
main.doStuff()
	/app/main.go:42 +0x1a
`
	details := AnalyzeFailure("go-test", output)
	if len(details) == 0 {
		t.Fatal("expected at least 1 failure detail")
	}
	d := details[0]
	if d.TestName != "panic" {
		t.Errorf("expected panic, got %s", d.TestName)
	}
	if !strings.Contains(d.ErrorMessage, "index out of range") {
		t.Errorf("expected panic message, got %s", d.ErrorMessage)
	}
	if d.StackTrace == "" {
		t.Error("expected stack trace")
	}
}

func TestAnalyzeFailure_Jest(t *testing.T) {
	output := `● App > should render correctly

    Expected: "hello"
    Received: "world"

      at Object.<anonymous> (src/App.test.js:10:5)
`
	details := AnalyzeFailure("jest", output)
	if len(details) == 0 {
		t.Fatal("expected at least 1 failure detail")
	}
	d := details[0]
	if !strings.Contains(d.TestName, "should render correctly") {
		t.Errorf("expected test name containing 'should render correctly', got %s", d.TestName)
	}
	if d.AssertionText == "" {
		t.Error("expected assertion text")
	}
	if d.FilePath == "" {
		t.Error("expected file path")
	}
}

func TestAnalyzeFailure_Pytest(t *testing.T) {
	output := `FAILED tests/test_app.py::test_login - AssertionError
> assert response.status_code == 200
E assert 401 == 200
`
	details := AnalyzeFailure("pytest", output)
	if len(details) == 0 {
		t.Fatal("expected at least 1 failure detail")
	}
	d := details[0]
	if d.TestName != "test_login - AssertionError" {
		t.Errorf("expected test_login, got %s", d.TestName)
	}
	if d.FilePath != "tests/test_app.py" {
		t.Errorf("expected tests/test_app.py, got %s", d.FilePath)
	}
}

func TestAnalyzeFailure_Generic(t *testing.T) {
	output := "some error output\nmore errors\nfinal line"
	details := AnalyzeFailure("unknown-framework", output)
	if len(details) != 1 {
		t.Fatalf("expected 1 detail, got %d", len(details))
	}
	if details[0].TestName != "unknown" {
		t.Errorf("expected unknown, got %s", details[0].TestName)
	}
}

func TestSuggestRootCauses_NilPointer(t *testing.T) {
	output := `goroutine 1 [running]:
runtime error: invalid memory address or nil pointer dereference
`
	causes := SuggestRootCauses("go-test", output)
	if len(causes) == 0 {
		t.Fatal("expected at least 1 root cause")
	}
	found := false
	for _, c := range causes {
		if c.Category == "nil_pointer" {
			found = true
			if c.Confidence < 0.9 {
				t.Errorf("expected high confidence, got %f", c.Confidence)
			}
		}
	}
	if !found {
		t.Error("expected nil_pointer category")
	}
}

func TestSuggestRootCauses_Timeout(t *testing.T) {
	output := "context deadline exceeded"
	causes := SuggestRootCauses("go-test", output)
	found := false
	for _, c := range causes {
		if c.Category == "timeout" {
			found = true
		}
	}
	if !found {
		t.Error("expected timeout category")
	}
}

func TestSuggestRootCauses_Assertion(t *testing.T) {
	output := `--- FAIL: TestFoo (0.01s)
    foo_test.go:10: expected 42, got 0
`
	causes := SuggestRootCauses("go-test", output)
	found := false
	for _, c := range causes {
		if c.Category == "assertion" {
			found = true
		}
	}
	if !found {
		t.Error("expected assertion category")
	}
}

func TestSuggestRootCauses_Network(t *testing.T) {
	output := "dial tcp 127.0.0.1:5432: connection refused"
	causes := SuggestRootCauses("go-test", output)
	found := false
	for _, c := range causes {
		if c.Category == "network" {
			found = true
		}
	}
	if !found {
		t.Error("expected network category")
	}
}

func TestSuggestRootCauses_Unknown(t *testing.T) {
	output := "some unrecognized error"
	causes := SuggestRootCauses("go-test", output)
	if len(causes) != 1 {
		t.Fatalf("expected 1 cause, got %d", len(causes))
	}
	if causes[0].Category != "unknown" {
		t.Errorf("expected unknown category, got %s", causes[0].Category)
	}
}

func TestCompareFailures_Identical(t *testing.T) {
	a := FailureDetail{
		TestName:      "TestFoo",
		FilePath:      "foo_test.go",
		ErrorMessage:  "expected 42 got 0",
		AssertionText: "assert.Equal(t, 42, result)",
	}
	sim := CompareFailures(a, a)
	if sim < 0.9 {
		t.Errorf("expected high similarity for identical failures, got %f", sim)
	}
}

func TestCompareFailures_Different(t *testing.T) {
	a := FailureDetail{
		TestName:     "TestFoo",
		FilePath:     "foo_test.go",
		ErrorMessage: "expected true got false",
	}
	b := FailureDetail{
		TestName:     "TestBar",
		FilePath:     "bar_test.go",
		ErrorMessage: "connection refused to database",
	}
	sim := CompareFailures(a, b)
	if sim > 0.3 {
		t.Errorf("expected low similarity for different failures, got %f", sim)
	}
}

func TestCompareFailures_SameTest(t *testing.T) {
	a := FailureDetail{
		TestName:     "TestFoo",
		FilePath:     "foo_test.go",
		ErrorMessage: "expected 42 got 0",
	}
	b := FailureDetail{
		TestName:     "TestFoo",
		FilePath:     "foo_test.go",
		ErrorMessage: "expected 42 got 1",
	}
	sim := CompareFailures(a, b)
	if sim < 0.5 {
		t.Errorf("expected moderate-high similarity for same test, got %f", sim)
	}
}

func TestStringSimilarity(t *testing.T) {
	tests := []struct {
		a, b    string
		minSim  float64
		maxSim  float64
	}{
		{"hello world", "hello world", 1.0, 1.0},
		{"hello world", "hello there", 0.2, 0.5},
		{"", "hello", 0.0, 0.0},
		{"completely different", "nothing alike here", 0.0, 0.1},
	}
	for _, tc := range tests {
		sim := stringSimilarity(tc.a, tc.b)
		if sim < tc.minSim || sim > tc.maxSim {
			t.Errorf("stringSimilarity(%q, %q) = %f, want [%f, %f]", tc.a, tc.b, sim, tc.minSim, tc.maxSim)
		}
	}
}

func TestFormatAnalysisForAgent_Empty(t *testing.T) {
	result := FormatAnalysisForAgent(nil)
	if result != "No test failures detected." {
		t.Errorf("expected 'No test failures detected.', got %q", result)
	}
}

func TestFormatAnalysisForAgent_WithResults(t *testing.T) {
	results := []AnalysisResult{
		{
			Failure: FailureDetail{
				TestName:      "TestFoo",
				FilePath:      "foo_test.go",
				ErrorMessage:  "expected 42 got 0",
				AssertionText: "assert.Equal(t, 42, result)",
			},
			RootCauses: []RootCause{
				{
					Description: "Assertion failure",
					Confidence:  0.85,
					Category:    "assertion",
					Suggestion:  "Check the logic",
				},
			},
			HistoricalMatches: []HistoricalMatch{
				{
					TestName:       "TestFoo",
					Similarity:     0.95,
					WasResolved:    true,
					FixDescription: "Fixed off-by-one error",
				},
			},
		},
	}

	formatted := FormatAnalysisForAgent(results)
	if !strings.Contains(formatted, "TestFoo") {
		t.Error("expected test name in output")
	}
	if !strings.Contains(formatted, "foo_test.go") {
		t.Error("expected file path in output")
	}
	if !strings.Contains(formatted, "Assertion failure") {
		t.Error("expected root cause in output")
	}
	if !strings.Contains(formatted, "Fixed off-by-one error") {
		t.Error("expected fix description in output")
	}
}
