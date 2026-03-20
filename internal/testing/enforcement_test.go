package testing

import "testing"

func TestParseCoverage_Go(t *testing.T) {
	output := `ok  	example.com/pkg1	0.5s	coverage: 78.5% of statements
ok  	example.com/pkg2	0.3s	coverage: 92.0% of statements
`
	cov := parseCoverage("go-test", output)
	if cov < 85 || cov > 86 {
		t.Errorf("expected ~85.25, got %.2f", cov)
	}
}

func TestParseCoverage_Jest(t *testing.T) {
	output := `All files | 85.71 | 80.00 | 90.00 | 85.71
`
	cov := parseCoverage("jest", output)
	if cov < 85 || cov > 86 {
		t.Errorf("expected ~85.71, got %.2f", cov)
	}
}

func TestParseCoverage_Pytest(t *testing.T) {
	output := `Name       Stmts   Miss  Cover
-----      -----   ----  -----
module.py     50     10    80%
TOTAL        100     15    85%
`
	cov := parseCoverage("pytest", output)
	if cov != 85 {
		t.Errorf("expected 85, got %.2f", cov)
	}
}

func TestParseCoverage_Unknown(t *testing.T) {
	cov := parseCoverage("unknown", "some output")
	if cov != 0 {
		t.Errorf("expected 0, got %.2f", cov)
	}
}

func TestExtractFailureDetails_Go(t *testing.T) {
	output := `--- FAIL: TestSomething (0.00s)
    file_test.go:10: expected true, got false
FAIL	example.com/pkg	0.5s
`
	details := extractFailureDetails("go-test", output)
	if details == "" {
		t.Error("expected non-empty failure details")
	}
	if !contains(details, "FAIL") {
		t.Error("expected FAIL in details")
	}
}

func TestAllTestsPassed(t *testing.T) {
	passed := []TestResult{{Passed: true}, {Passed: true}}
	if !AllTestsPassed(passed) {
		t.Error("expected all passed")
	}

	failed := []TestResult{{Passed: true}, {Passed: false}}
	if AllTestsPassed(failed) {
		t.Error("expected not all passed")
	}
}

func TestCoverageAboveThreshold(t *testing.T) {
	above := []TestResult{{Coverage: 90}, {Coverage: 85}}
	if !CoverageAboveThreshold(above) {
		t.Error("expected above threshold")
	}

	below := []TestResult{{Coverage: 90}, {Coverage: 50}}
	if CoverageAboveThreshold(below) {
		t.Error("expected below threshold")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && len(s) > 0 && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
