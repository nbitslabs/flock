package testing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// TestPattern represents a learned test pattern from the repository.
type TestPattern struct {
	Language       string   `json:"language"`
	NamingConvention string `json:"naming_convention"`
	FilePattern    string   `json:"file_pattern"`
	AssertionStyle string   `json:"assertion_style"`
	MockingPattern string   `json:"mocking_pattern,omitempty"`
	SetupPattern   string   `json:"setup_pattern,omitempty"`
	ExampleFiles   []string `json:"example_files"`
	ExampleContent []string `json:"example_content,omitempty"`
}

// TestPatternConfig stores learned patterns for a repository.
type TestPatternConfig struct {
	Patterns []TestPattern `json:"patterns"`
}

// LearnPatterns analyzes existing test files to extract patterns.
func LearnPatterns(repoPath string, config TestConfig) TestPatternConfig {
	result := TestPatternConfig{}

	for _, fw := range config.Frameworks {
		pattern := analyzeFramework(repoPath, fw)
		if pattern != nil {
			result.Patterns = append(result.Patterns, *pattern)
		}
	}

	return result
}

// analyzeFramework extracts patterns from test files for a specific framework.
func analyzeFramework(repoPath string, fw TestFramework) *TestPattern {
	pattern := &TestPattern{
		Language:    fw.Language,
		FilePattern: strings.Join(fw.FileGlobs, ", "),
	}

	// Collect test files
	var testFiles []string
	filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		// Skip vendor, node_modules, etc.
		if strings.Contains(path, "vendor/") || strings.Contains(path, "node_modules/") {
			return filepath.SkipDir
		}
		if isTestFile(path, fw) {
			rel, _ := filepath.Rel(repoPath, path)
			testFiles = append(testFiles, rel)
		}
		return nil
	})

	if len(testFiles) == 0 {
		return nil
	}

	// Analyze naming conventions
	pattern.NamingConvention = detectNamingConvention(testFiles, fw)

	// Analyze assertion and mocking styles from file content
	exampleFiles := testFiles
	if len(exampleFiles) > 3 {
		exampleFiles = exampleFiles[:3]
	}
	pattern.ExampleFiles = exampleFiles

	// Read example content
	for _, f := range exampleFiles {
		content, err := os.ReadFile(filepath.Join(repoPath, f))
		if err != nil {
			continue
		}
		// Limit to first 50 lines
		lines := strings.Split(string(content), "\n")
		if len(lines) > 50 {
			lines = lines[:50]
		}
		pattern.ExampleContent = append(pattern.ExampleContent, strings.Join(lines, "\n"))
	}

	// Detect assertion style
	pattern.AssertionStyle = detectAssertionStyle(repoPath, exampleFiles, fw)

	// Detect mocking
	pattern.MockingPattern = detectMockingPattern(repoPath, testFiles, fw)

	// Detect setup/teardown
	pattern.SetupPattern = detectSetupPattern(repoPath, testFiles, fw)

	return pattern
}

// isTestFile checks if a file matches the framework's test file patterns.
func isTestFile(path string, fw TestFramework) bool {
	name := filepath.Base(path)
	for _, glob := range fw.FileGlobs {
		// Simple matching
		if matched, _ := filepath.Match(filepath.Base(glob), name); matched {
			return true
		}
	}
	return false
}

// detectNamingConvention analyzes test file names to determine the convention.
func detectNamingConvention(files []string, fw TestFramework) string {
	switch fw.Language {
	case "go":
		return "file_test.go (TestFunctionName pattern)"
	case "javascript", "typescript":
		specCount := 0
		testCount := 0
		for _, f := range files {
			if strings.Contains(f, ".spec.") {
				specCount++
			}
			if strings.Contains(f, ".test.") {
				testCount++
			}
		}
		if specCount > testCount {
			return "*.spec.{js,ts} (describe/it pattern)"
		}
		return "*.test.{js,ts} (describe/it pattern)"
	case "python":
		return "test_*.py or *_test.py (def test_function pattern)"
	case "rust":
		return "#[cfg(test)] mod tests (fn test_name pattern)"
	case "java":
		return "*Test.java (@Test annotation pattern)"
	default:
		return "unknown"
	}
}

// Go assertion patterns
var goTestifyPattern = regexp.MustCompile(`assert\.\w+|require\.\w+`)
var goStdTestPattern = regexp.MustCompile(`t\.(Error|Fatal|Log|Helper|Run)`)

// detectAssertionStyle checks what assertion library/style is used.
func detectAssertionStyle(repoPath string, testFiles []string, fw TestFramework) string {
	for _, f := range testFiles {
		content, err := os.ReadFile(filepath.Join(repoPath, f))
		if err != nil {
			continue
		}
		s := string(content)

		switch fw.Language {
		case "go":
			if goTestifyPattern.MatchString(s) {
				return "testify (assert/require)"
			}
			if goStdTestPattern.MatchString(s) {
				return "standard testing (t.Error/t.Fatal)"
			}
		case "javascript", "typescript":
			if strings.Contains(s, "expect(") {
				if strings.Contains(s, "chai") {
					return "chai (expect)"
				}
				return "jest/vitest (expect)"
			}
		case "python":
			if strings.Contains(s, "assert ") {
				return "pytest (assert statements)"
			}
			if strings.Contains(s, "self.assert") {
				return "unittest (self.assert*)"
			}
		}
	}

	return "standard"
}

// detectMockingPattern checks for mocking libraries.
func detectMockingPattern(repoPath string, testFiles []string, fw TestFramework) string {
	for _, f := range testFiles {
		content, err := os.ReadFile(filepath.Join(repoPath, f))
		if err != nil {
			continue
		}
		s := string(content)

		switch fw.Language {
		case "go":
			if strings.Contains(s, "gomock") || strings.Contains(s, "mock.") {
				return "gomock"
			}
			if strings.Contains(s, "testify/mock") {
				return "testify/mock"
			}
		case "javascript", "typescript":
			if strings.Contains(s, "jest.mock") || strings.Contains(s, "jest.fn") {
				return "jest.mock/jest.fn"
			}
			if strings.Contains(s, "sinon") {
				return "sinon"
			}
		case "python":
			if strings.Contains(s, "unittest.mock") || strings.Contains(s, "mock.patch") {
				return "unittest.mock"
			}
			if strings.Contains(s, "pytest-mock") || strings.Contains(s, "mocker") {
				return "pytest-mock"
			}
		}
	}

	return ""
}

// detectSetupPattern checks for test setup/teardown patterns.
func detectSetupPattern(repoPath string, testFiles []string, fw TestFramework) string {
	for _, f := range testFiles {
		content, err := os.ReadFile(filepath.Join(repoPath, f))
		if err != nil {
			continue
		}
		s := string(content)

		switch fw.Language {
		case "go":
			if strings.Contains(s, "TestMain") {
				return "TestMain"
			}
			if strings.Contains(s, "t.Cleanup") {
				return "t.Cleanup"
			}
			if strings.Contains(s, "t.TempDir") {
				return "t.TempDir"
			}
		case "javascript", "typescript":
			if strings.Contains(s, "beforeEach") {
				return "beforeEach/afterEach"
			}
			if strings.Contains(s, "beforeAll") {
				return "beforeAll/afterAll"
			}
		case "python":
			if strings.Contains(s, "@pytest.fixture") {
				return "pytest fixtures"
			}
			if strings.Contains(s, "setUp") {
				return "setUp/tearDown"
			}
		}
	}

	return ""
}

// SavePatternConfig writes learned patterns to the repo state.
func SavePatternConfig(dataDir, org, repo string, config TestPatternConfig) error {
	statePath := filepath.Join(RepoStatePath(dataDir, org, repo), "test_patterns.json")
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pattern config: %w", err)
	}
	return os.WriteFile(statePath, data, 0o644)
}

// LoadPatternConfig reads learned patterns from repo state.
func LoadPatternConfig(dataDir, org, repo string) (*TestPatternConfig, error) {
	statePath := filepath.Join(RepoStatePath(dataDir, org, repo), "test_patterns.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, err
	}
	var config TestPatternConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse pattern config: %w", err)
	}
	return &config, nil
}
