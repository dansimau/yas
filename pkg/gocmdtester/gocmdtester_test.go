package gocmdtester_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"gotest.tools/v3/assert"
)

func TestMain(m *testing.M) {
	// Reset GOCMDTESTER_COVERPKG so tests that create temporary test modules
	// don't try to use a coverpkg pattern from the outer test harness.
	if err := os.Unsetenv("GOCMDTESTER_COVERPKG"); err != nil {
		panic(err)
	}

	code := m.Run()

	// Clean up all cached testers after all tests complete
	_ = gocmdtester.CleanupAll()

	os.Exit(code)
}

// createTestModule creates a temporary Go module directory with the given main.go code.
// Returns the path to the main.go file.
func createTestModule(t *testing.T, mainGoCode string) string {
	t.Helper()

	tmpDir := t.TempDir()

	// Create a simple Go module
	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(`module testapp
go 1.22
`), 0o644)
	assert.NilError(t, err)

	mainGo := filepath.Join(tmpDir, "main.go")
	err = os.WriteFile(mainGo, []byte(mainGoCode), 0o644)
	assert.NilError(t, err)

	return mainGo
}

// TestFromPath_Success tests successful compilation.
func TestFromPath_Success(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Verify binary exists
	_, err := os.Stat(tester.BinaryPath())
	assert.NilError(t, err)

	// Verify coverage directory exists
	_, err = os.Stat(tester.CoverageDir())
	assert.NilError(t, err)
}

// Note: TestFromPath_InvalidPath and TestFromPath_CompilationError were removed
// because FromPath now uses t.Fatal for errors, making error cases untestable
// without complex testing.T mocking.

// TestFromPath_CacheReturns tests that FromPath reuses the cached binary.
func TestFromPath_CacheReturns(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

import "fmt"

func main() {
	fmt.Println("cache test")
}
`)

	tester1 := gocmdtester.FromPath(t, mainGo)
	tester2 := gocmdtester.FromPath(t, mainGo)

	// Should share the same binary path (cached binary)
	assert.Equal(t, tester1.BinaryPath(), tester2.BinaryPath(), "FromPath should reuse cached binary")
	assert.Equal(t, tester1.CoverageDir(), tester2.CoverageDir(), "FromPath should reuse cached coverage dir")
}

// TestRun_Success tests successful command execution.
func TestRun_Success(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

import "fmt"

func main() {
	fmt.Println("success")
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	result := tester.Run()
	assert.Equal(t, result.ExitCode(), 0)
	assert.NilError(t, result.Err())
	assert.Assert(t, result.Success())
	assert.Assert(t, result.StdoutContains("success"))
}

// TestRun_Arguments tests that arguments are passed correctly.
func TestRun_Arguments(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	fmt.Println(strings.Join(os.Args[1:], " "))
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	result := tester.Run("hello", "world", "test")
	assert.Equal(t, result.ExitCode(), 0)
	assert.Assert(t, result.StdoutContains("hello world test"))
}

// TestFromPath_WithEnv tests environment variable handling.
func TestFromPath_WithEnv(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println(os.Getenv("TEST_VAR"))
}
`)

	tester := gocmdtester.FromPath(t, mainGo, gocmdtester.WithEnv("TEST_VAR", "test_value"))

	result := tester.Run()
	assert.Equal(t, result.ExitCode(), 0)
	assert.Assert(t, result.StdoutContains("test_value"))
}

// TestFromPath_WithMultipleEnv tests multiple environment variables.
func TestFromPath_WithMultipleEnv(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Printf("%s:%s", os.Getenv("VAR1"), os.Getenv("VAR2"))
}
`)

	tester := gocmdtester.FromPath(t, mainGo,
		gocmdtester.WithEnv("VAR1", "value1"),
		gocmdtester.WithEnv("VAR2", "value2"))

	result := tester.Run()
	assert.Equal(t, result.ExitCode(), 0)
	assert.Assert(t, result.StdoutContains("value1:value2"))
}

// TestFromPath_WithWorkingDir tests working directory handling.
func TestFromPath_WithWorkingDir(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	cwd, _ := os.Getwd()
	fmt.Println(cwd)
}
`)

	workDir := t.TempDir()
	tester := gocmdtester.FromPath(t, mainGo, gocmdtester.WithWorkingDir(workDir))

	result := tester.Run()
	assert.Equal(t, result.ExitCode(), 0)
	assert.Assert(t, strings.Contains(result.Stdout(), workDir))
}

// TestFromPath_WithStdin tests stdin handling.
func TestFromPath_WithStdin(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		fmt.Println("read:", scanner.Text())
	}
}
`)

	input := strings.NewReader("test input\n")
	tester := gocmdtester.FromPath(t, mainGo, gocmdtester.WithStdin(input))

	result := tester.Run()
	assert.Equal(t, result.ExitCode(), 0)
	assert.Assert(t, result.StdoutContains("read: test input"))
}

// TestRun_NonZeroExitCode tests that non-zero exit codes are captured correctly.
func TestRun_NonZeroExitCode(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "error message")
	os.Exit(42)
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	result := tester.Run()
	assert.Equal(t, result.ExitCode(), 42)
	assert.Error(t, result.Err(), "exit status 42")
	assert.Assert(t, !result.Success())
	assert.Assert(t, result.StderrContains("error message"))
}

// TestRun_StdoutStderr tests stdout and stderr capture.
func TestRun_StdoutStderr(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("stdout message")
	fmt.Fprintln(os.Stderr, "stderr message")
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	result := tester.Run()
	assert.Equal(t, result.ExitCode(), 0)
	assert.Assert(t, result.StdoutContains("stdout message"))
	assert.Assert(t, result.StderrContains("stderr message"))
	assert.Assert(t, !result.StdoutContains("stderr message"))
	assert.Assert(t, !result.StderrContains("stdout message"))
}

// TestResult_HelperMethods tests the Result helper methods.
func TestResult_HelperMethods(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

import "fmt"

func main() {
	fmt.Println("test output")
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	result := tester.Run()

	// Test Success()
	assert.Assert(t, result.Success())

	// Test StdoutContains()
	assert.Assert(t, result.StdoutContains("test"))
	assert.Assert(t, result.StdoutContains("output"))
	assert.Assert(t, !result.StdoutContains("missing"))

	// Test StderrContains()
	assert.Assert(t, !result.StderrContains("anything"))
}

// TestWriteCoverageProfile tests coverage collection and merging.
func TestWriteCoverageProfile(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

import "fmt"

func greet(name string) string {
	return "Hello, " + name
}

func main() {
	fmt.Println(greet("World"))
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Run the binary to generate coverage
	result := tester.Run()
	assert.Equal(t, result.ExitCode(), 0)

	// Write coverage profile
	coverFile := filepath.Join(t.TempDir(), "coverage.out")
	err := tester.WriteCoverageProfile(coverFile)
	assert.NilError(t, err)

	// Verify coverage file exists and has content
	content, err := os.ReadFile(coverFile)
	assert.NilError(t, err)
	assert.Assert(t, len(content) > 0)
	assert.Assert(t, strings.Contains(string(content), "mode:"))
}

// TestParallelExecution tests that multiple CmdTesters can run in parallel.
func TestParallelExecution(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

import "fmt"

func main() {
	fmt.Println("parallel test")
}
`)

	// Create multiple testers that run in parallel
	for i := range 3 {
		t.Run(fmt.Sprintf("parallel-%d", i), func(t *testing.T) {
			t.Parallel()

			tester := gocmdtester.FromPath(t, mainGo)

			result := tester.Run()
			assert.Equal(t, result.ExitCode(), 0)
			assert.Assert(t, result.StdoutContains("parallel test"))
		})
	}
}

// TestClearCache tests that ClearCache removes cached testers.
// NOTE: This test cannot run in parallel as it tests global cache cleanup.
func TestClearCache(t *testing.T) {
	// Clean up any existing cache state first
	_ = gocmdtester.CleanupAll()

	mainGo := createTestModule(t, `package main

func main() {}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	binaryPath := tester.BinaryPath()
	coverDir := tester.CoverageDir()

	// Verify they exist before cleanup
	_, err := os.Stat(binaryPath)
	assert.NilError(t, err)
	_, err = os.Stat(coverDir)
	assert.NilError(t, err)

	// Clear cache
	err = gocmdtester.CleanupAll()
	assert.NilError(t, err)

	// Verify they no longer exist
	_, err = os.Stat(binaryPath)
	assert.Assert(t, os.IsNotExist(err))
	_, err = os.Stat(coverDir)
	assert.Assert(t, os.IsNotExist(err))
}

// TestCleanup_IsNoOp tests that Cleanup() is a no-op for cached testers.
func TestCleanup_IsNoOp(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

func main() {}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	binaryPath := tester.BinaryPath()
	coverDir := tester.CoverageDir()

	// Call Cleanup - should be a no-op
	err := tester.Cleanup()
	assert.NilError(t, err)

	// Files should still exist (Cleanup is a no-op)
	_, err = os.Stat(binaryPath)
	assert.NilError(t, err)
	_, err = os.Stat(coverDir)
	assert.NilError(t, err)
}

// TestFromPath_CombinedOptions tests multiple options together.
func TestFromPath_CombinedOptions(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	cwd, _ := os.Getwd()
	fmt.Println("cwd:", cwd)
	fmt.Println("env:", os.Getenv("CUSTOM_VAR"))

	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		fmt.Println("stdin:", scanner.Text())
	}
}
`)

	workDir := t.TempDir()
	input := strings.NewReader("input data\n")

	tester := gocmdtester.FromPath(t, mainGo,
		gocmdtester.WithWorkingDir(workDir),
		gocmdtester.WithEnv("CUSTOM_VAR", "custom_value"),
		gocmdtester.WithStdin(input))

	result := tester.Run()

	assert.Assert(t, result.Success())
	assert.Assert(t, result.StdoutContains(workDir))
	assert.Assert(t, result.StdoutContains("custom_value"))
	assert.Assert(t, result.StdoutContains("input data"))
}

// TestMultipleRuns tests that the same tester can be used for multiple runs.
func TestMultipleRuns(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 {
		fmt.Println(os.Args[1])
	} else {
		fmt.Println("no args")
	}
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// First run
	result1 := tester.Run()
	assert.Assert(t, result1.Success())
	assert.Assert(t, result1.StdoutContains("no args"))

	// Second run with different args
	result2 := tester.Run("with-arg")
	assert.Assert(t, result2.Success())
	assert.Assert(t, result2.StdoutContains("with-arg"))

	// Third run with different args
	result3 := tester.Run("another")
	assert.Assert(t, result3.Success())
	assert.Assert(t, result3.StdoutContains("another"))
}

// TestWriteCoverageProfile_MultipleCalls tests that coverage accumulates.
func TestWriteCoverageProfile_MultipleCalls(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

import (
	"fmt"
	"os"
)

func feature1() {
	fmt.Println("feature 1")
}

func feature2() {
	fmt.Println("feature 2")
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "f2" {
		feature2()
	} else {
		feature1()
	}
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Run with feature 1
	result1 := tester.Run()
	assert.Assert(t, result1.Success())

	// Run with feature 2
	result2 := tester.Run("f2")
	assert.Assert(t, result2.Success())

	// Write coverage profile
	coverFile := filepath.Join(t.TempDir(), "coverage.out")
	err := tester.WriteCoverageProfile(coverFile)
	assert.NilError(t, err)

	// Verify coverage file contains data
	content, err := os.ReadFile(coverFile)
	assert.NilError(t, err)
	assert.Assert(t, len(content) > 0)

	// Basic sanity check that it's a coverage file
	contentStr := string(content)
	assert.Assert(t, strings.Contains(contentStr, "mode:"))
}

// TestWriteCombinedCoverage tests combined coverage from multiple testers.
func TestWriteCombinedCoverage(t *testing.T) {
	t.Parallel()

	mainGo1 := createTestModule(t, `package main

import "fmt"

func main() {
	fmt.Println("app1")
}
`)

	mainGo2 := createTestModule(t, `package main

import "fmt"

func main() {
	fmt.Println("app2")
}
`)

	tester1 := gocmdtester.FromPath(t, mainGo1)
	tester2 := gocmdtester.FromPath(t, mainGo2)

	// Run both binaries
	result1 := tester1.Run()
	assert.Assert(t, result1.Success())

	result2 := tester2.Run()
	assert.Assert(t, result2.Success())

	// Write combined coverage
	coverFile := filepath.Join(t.TempDir(), "combined-coverage.out")
	err := gocmdtester.WriteCombinedCoverage(coverFile)
	assert.NilError(t, err)

	// Verify coverage file exists and has content
	content, err := os.ReadFile(coverFile)
	assert.NilError(t, err)
	assert.Assert(t, len(content) > 0)
	assert.Assert(t, strings.Contains(string(content), "mode:"))
}

// TestGOCOVERDIR_Isolation tests that GOCOVERDIR is properly isolated.
func TestGOCOVERDIR_Isolation(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println(os.Getenv("GOCOVERDIR"))
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	result := tester.Run()
	assert.Assert(t, result.Success())

	// Verify GOCOVERDIR is set to the tester's coverage directory
	assert.Assert(t, result.StdoutContains(tester.CoverageDir()))
}

// TestAccessorMethods tests the BinaryPath and CoverageDir accessor methods.
func TestAccessorMethods(t *testing.T) {
	t.Parallel()

	mainGo := createTestModule(t, `package main

func main() {}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Test BinaryPath
	binaryPath := tester.BinaryPath()
	assert.Assert(t, binaryPath != "")
	_, err := os.Stat(binaryPath)
	assert.NilError(t, err)

	// Test CoverageDir
	coverDir := tester.CoverageDir()
	assert.Assert(t, coverDir != "")
	_, err = os.Stat(coverDir)
	assert.NilError(t, err)
}
