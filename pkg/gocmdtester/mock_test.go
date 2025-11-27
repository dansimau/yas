package gocmdtester_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"gotest.tools/v3/assert"
)

// createTestModuleWithExec creates a temporary Go module that executes external commands.
func createTestModuleWithExec(t *testing.T, mainGoCode string) string {
	t.Helper()

	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(`module testapp
go 1.22
`), 0o644)
	assert.NilError(t, err)

	mainGo := filepath.Join(tmpDir, "main.go")
	err = os.WriteFile(mainGo, []byte(mainGoCode), 0o644)
	assert.NilError(t, err)

	return mainGo
}

// TestMock_BasicMock tests basic mock functionality.
func TestMock_BasicMock(t *testing.T) {
	t.Parallel()

	// Create a test binary that calls an external command
	mainGo := createTestModuleWithExec(t, `package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	cmd := exec.Command("gh", "pr", "create")
	output, err := cmd.Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Print(string(output))
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Set up mock
	mock := tester.Mock("gh", "pr", "create").
		WithStdout("https://github.com/user/repo/pull/1\n").
		WithCode(0)

	// Run the binary
	result := tester.Run()

	// Verify the mock was called
	assert.Assert(t, mock.Called(), "expected mock to be called")
	assert.Equal(t, mock.CalledTimes(), 1)
	assert.Assert(t, result.Success(), "expected success, got exit code %d, stderr: %s", result.ExitCode(), result.Stderr())
	assert.Assert(t, result.StdoutContains("https://github.com/user/repo/pull/1"))
}

// TestMock_NoMatch tests that unmatched commands exit with 254.
func TestMock_NoMatch(t *testing.T) {
	t.Parallel()

	// Create a test binary that calls an external command
	mainGo := createTestModuleWithExec(t, `package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	cmd := exec.Command("gh", "pr", "list")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "command failed: %s\n", string(output))
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
	fmt.Print(string(output))
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Skip verification since this test intentionally has an uncalled mock
	tester.SkipMockVerification()

	// Set up mock for different args
	mock := tester.Mock("gh", "pr", "create").
		WithStdout("https://github.com/user/repo/pull/1\n")

	// Run the binary - it calls "gh pr list" which doesn't match our mock
	result := tester.Run()

	// Mock should not be called
	assert.Assert(t, !mock.Called(), "mock should not be called")

	// Binary should fail because mockshim returns 254 for no match
	assert.Equal(t, result.ExitCode(), 254)
}

// TestMock_ExitCode tests mock exit code handling.
func TestMock_ExitCode(t *testing.T) {
	t.Parallel()

	mainGo := createTestModuleWithExec(t, `package main

import (
	"os"
	"os/exec"
)

func main() {
	cmd := exec.Command("failing-cmd")
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Set up mock with non-zero exit code - no args
	mock := tester.Mock("failing-cmd").
		WithStderr("command failed\n").
		WithCode(42)

	result := tester.Run()
	assert.Equal(t, result.ExitCode(), 42)
	assert.Assert(t, mock.Called())
}

// TestMock_MultipleMocks tests multiple mocks for different commands.
func TestMock_MultipleMocks(t *testing.T) {
	t.Parallel()

	mainGo := createTestModuleWithExec(t, `package main

import (
	"fmt"
	"os/exec"
)

func main() {
	// Call git
	gitCmd := exec.Command("git", "push")
	gitOut, _ := gitCmd.Output()
	fmt.Print("git: " + string(gitOut))

	// Call gh
	ghCmd := exec.Command("gh", "pr", "create")
	ghOut, _ := ghCmd.Output()
	fmt.Print("gh: " + string(ghOut))
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Set up mocks for both commands
	gitMock := tester.Mock("git", "push").WithStdout("pushed\n")
	ghMock := tester.Mock("gh", "pr", "create").WithStdout("PR created\n")

	result := tester.Run()

	assert.Assert(t, result.Success())
	assert.Assert(t, gitMock.Called())
	assert.Assert(t, ghMock.Called())
	assert.Assert(t, result.StdoutContains("git: pushed"))
	assert.Assert(t, result.StdoutContains("gh: PR created"))
}

// TestMock_SameCommandDifferentArgs tests multiple mocks for the same command with different args.
func TestMock_SameCommandDifferentArgs(t *testing.T) {
	t.Parallel()

	mainGo := createTestModuleWithExec(t, `package main

import (
	"fmt"
	"os/exec"
)

func main() {
	// Call gh pr create
	cmd1 := exec.Command("gh", "pr", "create")
	out1, _ := cmd1.Output()
	fmt.Print("create: " + string(out1))

	// Call gh pr list
	cmd2 := exec.Command("gh", "pr", "list")
	out2, _ := cmd2.Output()
	fmt.Print("list: " + string(out2))
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Set up mocks for same command with different args
	createMock := tester.Mock("gh", "pr", "create").WithStdout("PR #1\n")
	listMock := tester.Mock("gh", "pr", "list").WithStdout("No PRs\n")

	result := tester.Run()

	assert.Assert(t, result.Success())
	assert.Assert(t, createMock.Called())
	assert.Assert(t, listMock.Called())
	assert.Assert(t, result.StdoutContains("create: PR #1"))
	assert.Assert(t, result.StdoutContains("list: No PRs"))
}

// TestMock_CalledWithArgs tests the CalledWithArgs method.
func TestMock_CalledWithArgs(t *testing.T) {
	t.Parallel()

	mainGo := createTestModuleWithExec(t, `package main

import (
	"os/exec"
)

func main() {
	exec.Command("cmd", "arg1", "arg2").Run()
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	mock := tester.Mock("cmd", "arg1", "arg2")

	tester.Run()

	assert.Assert(t, mock.CalledWithArgs("arg1", "arg2"))
	assert.Assert(t, !mock.CalledWithArgs("arg1"))
	assert.Assert(t, !mock.CalledWithArgs("different"))
}

// TestMock_MultipleRuns tests that mocks work across multiple Run() calls.
func TestMock_MultipleRuns(t *testing.T) {
	t.Parallel()

	mainGo := createTestModuleWithExec(t, `package main

import (
	"fmt"
	"os/exec"
)

func main() {
	cmd := exec.Command("test-cmd")
	out, _ := cmd.Output()
	fmt.Print(string(out))
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	mock := tester.Mock("test-cmd").WithStdout("output\n")

	// Run multiple times
	tester.Run()
	tester.Run()
	tester.Run()

	// Mock should have been called 3 times
	assert.Equal(t, mock.CalledTimes(), 3)
}

// TestMock_Calls tests getting all calls to a mock.
func TestMock_Calls(t *testing.T) {
	t.Parallel()

	mainGo := createTestModuleWithExec(t, `package main

import (
	"os/exec"
)

func main() {
	exec.Command("test-cmd", "first").Run()
	exec.Command("test-cmd", "second").Run()
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	mock1 := tester.Mock("test-cmd", "first")
	mock2 := tester.Mock("test-cmd", "second")

	tester.Run()

	calls1 := mock1.Calls()
	calls2 := mock2.Calls()

	assert.Equal(t, len(calls1), 1)
	assert.Equal(t, len(calls2), 1)
	assert.Equal(t, calls1[0].Command, "test-cmd")
	assert.DeepEqual(t, calls1[0].Args, []string{"first"})
	assert.DeepEqual(t, calls2[0].Args, []string{"second"})
}

// TestMock_WithPassthroughExec tests that passthrough executes the real command.
func TestMock_WithPassthroughExec(t *testing.T) {
	t.Parallel()

	// Create a test binary that calls "echo" which is a real command
	mainGo := createTestModuleWithExec(t, `package main

import (
	"fmt"
	"os/exec"
)

func main() {
	cmd := exec.Command("echo", "hello", "from", "passthrough")
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Print("output: " + string(output))
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Set up passthrough mock for echo
	mock := tester.Mock("echo", "hello", "from", "passthrough").WithPassthroughExec()

	result := tester.Run()

	// Mock should have been called
	assert.Assert(t, mock.Called(), "expected mock to be called")

	// The real echo command should have executed
	assert.Assert(t, result.Success(), "expected success, got exit code %d, stderr: %s", result.ExitCode(), result.Stderr())
	assert.Assert(t, result.StdoutContains("hello from passthrough"), "expected output from real echo command")
}

// TestMock_PassthroughExitCode tests that passthrough returns the real command's exit code.
func TestMock_PassthroughExitCode(t *testing.T) {
	t.Parallel()

	// Create a test binary that calls "false" which exits with code 1
	mainGo := createTestModuleWithExec(t, `package main

import (
	"os"
	"os/exec"
)

func main() {
	cmd := exec.Command("false")
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Set up passthrough mock for false
	mock := tester.Mock("false").WithPassthroughExec()

	result := tester.Run()

	// Mock should have been called
	assert.Assert(t, mock.Called(), "expected mock to be called")

	// The real "false" command returns exit code 1
	assert.Equal(t, result.ExitCode(), 1, "expected exit code 1 from false command")
}

// TestMock_VerifyAllCalled_Success tests that verification passes when all mocks are called.
func TestMock_VerifyAllCalled_Success(t *testing.T) {
	t.Parallel()

	mainGo := createTestModuleWithExec(t, `package main

import (
	"os/exec"
)

func main() {
	exec.Command("cmd1").Run()
	exec.Command("cmd2", "arg").Run()
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Set up mocks that will be called
	tester.Mock("cmd1").WithCode(0)
	tester.Mock("cmd2", "arg").WithCode(0)

	tester.Run()

	// Test should pass - verification happens at t.Cleanup()
	// If mocks weren't called, the test would fail during cleanup
}

// TestMock_AnyMatcher tests the Any matcher that matches exactly one argument.
func TestMock_AnyMatcher(t *testing.T) {
	t.Parallel()

	mainGo := createTestModuleWithExec(t, `package main

import (
	"fmt"
	"os/exec"
)

func main() {
	// Call with one argument - should match
	cmd := exec.Command("git", "merge-base", "HEAD")
	out, _ := cmd.Output()
	fmt.Print(string(out))
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Set up mock with Any matcher
	mock := tester.Mock("git", "merge-base", gocmdtester.Any).
		WithStdout("abc123\n")

	result := tester.Run()

	assert.Assert(t, result.Success(), "expected success, got exit code %d, stderr: %s", result.ExitCode(), result.Stderr())
	assert.Assert(t, mock.Called(), "expected mock to be called")
	assert.Assert(t, result.StdoutContains("abc123"))
}

// TestMock_AnyMatcher_NoMatch tests that Any doesn't match zero or two arguments.
func TestMock_AnyMatcher_NoMatch(t *testing.T) {
	t.Parallel()

	mainGo := createTestModuleWithExec(t, `package main

import (
	"os"
	"os/exec"
)

func main() {
	// Call with two arguments - should NOT match the mock with only one Any
	cmd := exec.Command("git", "merge-base", "HEAD", "main")
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}
`)

	tester := gocmdtester.FromPath(t, mainGo)
	tester.SkipMockVerification()

	// Set up mock with Any matcher - should NOT match two args
	mock := tester.Mock("git", "merge-base", gocmdtester.Any).
		WithStdout("abc123\n")

	result := tester.Run()

	// Should return 254 (no match)
	assert.Equal(t, result.ExitCode(), 254)
	assert.Assert(t, !mock.Called(), "mock should not be called")
}

// TestMock_AnyFurtherArgs tests the AnyFurtherArgs matcher.
func TestMock_AnyFurtherArgs(t *testing.T) {
	t.Parallel()

	mainGo := createTestModuleWithExec(t, `package main

import (
	"fmt"
	"os/exec"
)

func main() {
	// Call with multiple arguments
	cmd := exec.Command("git", "push", "origin", "main", "--force")
	out, _ := cmd.Output()
	fmt.Print(string(out))
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Set up mock with AnyFurtherArgs matcher
	mock := tester.Mock("git", "push", gocmdtester.AnyFurtherArgs).
		WithStdout("pushed\n")

	result := tester.Run()

	assert.Assert(t, result.Success(), "expected success, got exit code %d, stderr: %s", result.ExitCode(), result.Stderr())
	assert.Assert(t, mock.Called(), "expected mock to be called")
	assert.Assert(t, result.StdoutContains("pushed"))
}

// TestMock_AnyFurtherArgs_ZeroArgs tests that AnyFurtherArgs matches zero additional arguments.
func TestMock_AnyFurtherArgs_ZeroArgs(t *testing.T) {
	t.Parallel()

	mainGo := createTestModuleWithExec(t, `package main

import (
	"fmt"
	"os/exec"
)

func main() {
	// Call with just "git push" - no additional args
	cmd := exec.Command("git", "push")
	out, _ := cmd.Output()
	fmt.Print(string(out))
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Set up mock with AnyFurtherArgs - should match even with zero additional args
	mock := tester.Mock("git", "push", gocmdtester.AnyFurtherArgs).
		WithStdout("pushed\n")

	result := tester.Run()

	assert.Assert(t, result.Success(), "expected success, got exit code %d, stderr: %s", result.ExitCode(), result.Stderr())
	assert.Assert(t, mock.Called(), "expected mock to be called")
}

// TestMock_PatternWithPassthrough tests pattern matching combined with passthrough.
func TestMock_PatternWithPassthrough(t *testing.T) {
	t.Parallel()

	mainGo := createTestModuleWithExec(t, `package main

import (
	"fmt"
	"os/exec"
)

func main() {
	cmd := exec.Command("echo", "hello", "world")
	out, _ := cmd.Output()
	fmt.Print("output: " + string(out))
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Set up mock with pattern matching and passthrough
	mock := tester.Mock("echo", gocmdtester.AnyFurtherArgs).
		WithPassthroughExec()

	result := tester.Run()

	assert.Assert(t, result.Success(), "expected success, got exit code %d, stderr: %s", result.ExitCode(), result.Stderr())
	assert.Assert(t, mock.Called(), "expected mock to be called")
	assert.Assert(t, result.StdoutContains("hello world"), "expected real echo output")
}

// TestMock_PatternPriority tests that mocks are matched in registration order.
func TestMock_PatternPriority(t *testing.T) {
	t.Parallel()

	mainGo := createTestModuleWithExec(t, `package main

import (
	"fmt"
	"os/exec"
)

func main() {
	// This should match the specific mock, not the general one
	cmd := exec.Command("git", "push", "origin")
	out, _ := cmd.Output()
	fmt.Print(string(out))
}
`)

	tester := gocmdtester.FromPath(t, mainGo)
	tester.SkipMockVerification() // General mock won't be called

	// Register specific mock FIRST
	specificMock := tester.Mock("git", "push", "origin").
		WithStdout("specific\n")

	// Register general mock SECOND - it won't be executed but its pattern matches
	tester.Mock("git", "push", gocmdtester.AnyFurtherArgs).
		WithStdout("general\n")

	result := tester.Run()

	assert.Assert(t, result.Success())
	assert.Assert(t, specificMock.Called(), "specific mock should be called")
	// The specific mock's output should be used (not general)
	assert.Assert(t, result.StdoutContains("specific"))
	assert.Assert(t, !result.StdoutContains("general"), "general mock output should not appear")
}

// TestMock_MultipleAnyMatchers tests using multiple Any matchers.
func TestMock_MultipleAnyMatchers(t *testing.T) {
	t.Parallel()

	mainGo := createTestModuleWithExec(t, `package main

import (
	"fmt"
	"os/exec"
)

func main() {
	cmd := exec.Command("git", "diff", "HEAD", "main")
	out, _ := cmd.Output()
	fmt.Print(string(out))
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Set up mock with multiple Any matchers
	mock := tester.Mock("git", "diff", gocmdtester.Any, gocmdtester.Any).
		WithStdout("diff output\n")

	result := tester.Run()

	assert.Assert(t, result.Success(), "expected success, got exit code %d, stderr: %s", result.ExitCode(), result.Stderr())
	assert.Assert(t, mock.Called(), "expected mock to be called")
	assert.Assert(t, result.StdoutContains("diff output"))
}

// TestMock_CalledWithArgs_PatternMock tests CalledWithArgs with pattern-based mocks.
func TestMock_CalledWithArgs_PatternMock(t *testing.T) {
	t.Parallel()

	mainGo := createTestModuleWithExec(t, `package main

import (
	"os/exec"
)

func main() {
	exec.Command("git", "push", "origin", "main").Run()
	exec.Command("git", "push", "upstream", "feature").Run()
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Set up mock with AnyFurtherArgs
	mock := tester.Mock("git", "push", gocmdtester.AnyFurtherArgs)

	tester.Run()

	// Verify specific invocations
	assert.Assert(t, mock.CalledWithArgs("push", "origin", "main"), "should have been called with origin main")
	assert.Assert(t, mock.CalledWithArgs("push", "upstream", "feature"), "should have been called with upstream feature")
	assert.Assert(t, !mock.CalledWithArgs("push", "other"), "should not have been called with other")
	assert.Equal(t, mock.CalledTimes(), 2)
}

// TestMock_Calls_PatternMock tests the Calls() method with pattern-based mocks.
func TestMock_Calls_PatternMock(t *testing.T) {
	t.Parallel()

	mainGo := createTestModuleWithExec(t, `package main

import (
	"os/exec"
)

func main() {
	exec.Command("git", "merge-base", "HEAD").Run()
	exec.Command("git", "merge-base", "main").Run()
	exec.Command("git", "status").Run()
}
`)

	tester := gocmdtester.FromPath(t, mainGo)

	// Set up pattern mock
	mergeBaseMock := tester.Mock("git", "merge-base", gocmdtester.Any)
	statusMock := tester.Mock("git", "status")

	tester.Run()

	// Check Calls() returns all matching invocations
	calls := mergeBaseMock.Calls()
	assert.Equal(t, len(calls), 2)
	assert.DeepEqual(t, calls[0].Args, []string{"merge-base", "HEAD"})
	assert.DeepEqual(t, calls[1].Args, []string{"merge-base", "main"})

	// status mock should have one call
	assert.Equal(t, statusMock.CalledTimes(), 1)
}
