package testing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TestFramework represents a detected test framework.
type TestFramework struct {
	Name       string   `json:"name"`
	Language   string   `json:"language"`
	Command    string   `json:"command"`
	ConfigFile string   `json:"config_file,omitempty"`
	TestDirs   []string `json:"test_dirs,omitempty"`
	FileGlobs  []string `json:"file_globs"`
}

// TestConfig holds the discovered test configuration for a repository.
type TestConfig struct {
	Frameworks []TestFramework `json:"frameworks"`
	RepoPath   string          `json:"repo_path"`
}

// DiscoverTests scans a repository for test frameworks and returns the config.
func DiscoverTests(repoPath string) TestConfig {
	config := TestConfig{RepoPath: repoPath}

	// Check for Go tests
	if fw := detectGo(repoPath); fw != nil {
		config.Frameworks = append(config.Frameworks, *fw)
	}

	// Check for Node.js/JavaScript tests
	if fws := detectNodeJS(repoPath); len(fws) > 0 {
		config.Frameworks = append(config.Frameworks, fws...)
	}

	// Check for Python tests
	if fw := detectPython(repoPath); fw != nil {
		config.Frameworks = append(config.Frameworks, *fw)
	}

	// Check for Rust tests
	if fw := detectRust(repoPath); fw != nil {
		config.Frameworks = append(config.Frameworks, *fw)
	}

	// Check for Java tests
	if fw := detectJava(repoPath); fw != nil {
		config.Frameworks = append(config.Frameworks, *fw)
	}

	return config
}

// detectGo checks for Go test files.
func detectGo(repoPath string) *TestFramework {
	// Check for go.mod
	if _, err := os.Stat(filepath.Join(repoPath, "go.mod")); err != nil {
		return nil
	}

	// Find _test.go files
	var testDirs []string
	seen := make(map[string]bool)

	filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			dir := filepath.Dir(path)
			rel, _ := filepath.Rel(repoPath, dir)
			if !seen[rel] {
				seen[rel] = true
				testDirs = append(testDirs, rel)
			}
		}
		return nil
	})

	if len(testDirs) == 0 {
		return nil
	}

	return &TestFramework{
		Name:      "go-test",
		Language:  "go",
		Command:   "go test ./...",
		TestDirs:  testDirs,
		FileGlobs: []string{"*_test.go"},
	}
}

// detectNodeJS checks for Node.js test frameworks.
func detectNodeJS(repoPath string) []TestFramework {
	pkgPath := filepath.Join(repoPath, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return nil
	}

	var pkg struct {
		Scripts      map[string]string `json:"scripts"`
		Dependencies map[string]string `json:"dependencies"`
		DevDeps      map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}

	var frameworks []TestFramework

	// Check for Jest
	if _, ok := pkg.DevDeps["jest"]; ok {
		cmd := "npx jest"
		if testScript, ok := pkg.Scripts["test"]; ok && strings.Contains(testScript, "jest") {
			cmd = "npm test"
		}
		fw := TestFramework{
			Name:       "jest",
			Language:   "javascript",
			Command:    cmd,
			ConfigFile: "package.json",
			FileGlobs:  []string{"**/*.test.js", "**/*.test.ts", "**/*.spec.js", "**/*.spec.ts"},
		}

		// Check for __tests__ directories
		if entries, err := os.ReadDir(repoPath); err == nil {
			for _, e := range entries {
				if e.IsDir() && e.Name() == "__tests__" {
					fw.TestDirs = append(fw.TestDirs, "__tests__")
				}
			}
		}

		frameworks = append(frameworks, fw)
	}

	// Check for Vitest
	if _, ok := pkg.DevDeps["vitest"]; ok {
		frameworks = append(frameworks, TestFramework{
			Name:       "vitest",
			Language:   "typescript",
			Command:    "npx vitest run",
			ConfigFile: "package.json",
			FileGlobs:  []string{"**/*.test.ts", "**/*.spec.ts"},
		})
	}

	// Check for Mocha
	if _, ok := pkg.DevDeps["mocha"]; ok {
		frameworks = append(frameworks, TestFramework{
			Name:      "mocha",
			Language:  "javascript",
			Command:   "npx mocha",
			FileGlobs: []string{"test/**/*.js", "test/**/*.ts"},
			TestDirs:  []string{"test"},
		})
	}

	// Fallback: if there's a test script but no known framework detected
	if len(frameworks) == 0 {
		if testScript, ok := pkg.Scripts["test"]; ok && testScript != "" {
			frameworks = append(frameworks, TestFramework{
				Name:      "npm-test",
				Language:  "javascript",
				Command:   "npm test",
				FileGlobs: []string{"**/*.test.*", "**/*.spec.*"},
			})
		}
	}

	return frameworks
}

// detectPython checks for Python test frameworks.
func detectPython(repoPath string) *TestFramework {
	// Check for pytest
	for _, cfg := range []string{"pytest.ini", "pyproject.toml", "setup.cfg"} {
		if _, err := os.Stat(filepath.Join(repoPath, cfg)); err == nil {
			return &TestFramework{
				Name:       "pytest",
				Language:   "python",
				Command:    "pytest",
				ConfigFile: cfg,
				FileGlobs:  []string{"test_*.py", "*_test.py", "tests/**/*.py"},
				TestDirs:   findDirs(repoPath, "tests", "test"),
			}
		}
	}

	// Check for test directories
	dirs := findDirs(repoPath, "tests", "test")
	if len(dirs) > 0 {
		return &TestFramework{
			Name:      "pytest",
			Language:  "python",
			Command:   "pytest",
			FileGlobs: []string{"test_*.py", "*_test.py"},
			TestDirs:  dirs,
		}
	}

	return nil
}

// detectRust checks for Rust tests.
func detectRust(repoPath string) *TestFramework {
	if _, err := os.Stat(filepath.Join(repoPath, "Cargo.toml")); err != nil {
		return nil
	}

	return &TestFramework{
		Name:       "cargo-test",
		Language:   "rust",
		Command:    "cargo test",
		ConfigFile: "Cargo.toml",
		FileGlobs:  []string{"**/*.rs"},
	}
}

// detectJava checks for Java test frameworks.
func detectJava(repoPath string) *TestFramework {
	// Maven
	if _, err := os.Stat(filepath.Join(repoPath, "pom.xml")); err == nil {
		return &TestFramework{
			Name:       "maven",
			Language:   "java",
			Command:    "mvn test",
			ConfigFile: "pom.xml",
			FileGlobs:  []string{"**/*Test.java", "**/*Tests.java"},
			TestDirs:   []string{"src/test"},
		}
	}

	// Gradle
	if _, err := os.Stat(filepath.Join(repoPath, "build.gradle")); err == nil {
		return &TestFramework{
			Name:       "gradle",
			Language:   "java",
			Command:    "./gradlew test",
			ConfigFile: "build.gradle",
			FileGlobs:  []string{"**/*Test.java", "**/*Tests.java"},
			TestDirs:   []string{"src/test"},
		}
	}

	return nil
}

// findDirs returns which of the given directory names exist under root.
func findDirs(root string, names ...string) []string {
	var found []string
	for _, name := range names {
		if info, err := os.Stat(filepath.Join(root, name)); err == nil && info.IsDir() {
			found = append(found, name)
		}
	}
	return found
}

// SaveTestConfig writes the discovered test config to the repo state directory.
func SaveTestConfig(dataDir, org, repo string, config TestConfig) error {
	statePath := filepath.Join(RepoStatePath(dataDir, org, repo), "test_config.json")
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal test config: %w", err)
	}
	return os.WriteFile(statePath, data, 0o644)
}

// LoadTestConfig reads the test config from the repo state directory.
func LoadTestConfig(dataDir, org, repo string) (*TestConfig, error) {
	statePath := filepath.Join(RepoStatePath(dataDir, org, repo), "test_config.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, err
	}
	var config TestConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse test config: %w", err)
	}
	return &config, nil
}

// RepoStatePath returns the repo-specific state directory path.
// Re-exported from memory package for convenience.
func RepoStatePath(dataDir, org, repo string) string {
	return filepath.Join(resolveStateDir(dataDir), "state", "github.com", org, repo)
}

func resolveStateDir(dataDir string) string {
	if strings.HasSuffix(dataDir, ".flock") {
		return dataDir
	}
	return filepath.Join(dataDir, ".flock")
}
