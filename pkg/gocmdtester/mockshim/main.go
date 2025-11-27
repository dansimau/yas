// Package main implements the MockShim binary for gocmdtester.
// This binary is used to intercept command executions during tests.
//
// It reads configuration from $GOCMDTESTER_MOCK_DIR/mock_config.json,
// logs all invocations to $GOCMDTESTER_MOCK_DIR/mock_invocations_<pid>.ndjson,
// and returns configured stdout/stderr/exit code for matching mocks.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Matcher constants - must match the values in gocmdtester/matchers.go.
const (
	matcherAny            = "__MOCKSHIM_ANY__"
	matcherAnyFurtherArgs = "__MOCKSHIM_ANY_FURTHER__"
)

// MockConfig represents a single mock configuration.
type MockConfig struct {
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	Stdout      string   `json:"stdout"`
	Stderr      string   `json:"stderr"`
	ExitCode    int      `json:"exitCode"`
	Passthrough bool     `json:"passthrough"`
}

// Invocation represents a recorded command invocation.
type Invocation struct {
	Command   string    `json:"command"`
	Args      []string  `json:"args"`
	Timestamp time.Time `json:"timestamp"`
}

func main() {
	exitCode := run()
	os.Exit(exitCode)
}

func run() int {
	mockDir := os.Getenv("GOCMDTESTER_MOCK_DIR")
	if mockDir == "" {
		fmt.Fprintln(os.Stderr, "mockshim: GOCMDTESTER_MOCK_DIR not set")

		return 255
	}

	// Determine which command we're pretending to be (from argv[0])
	command := filepath.Base(os.Args[0])
	args := os.Args[1:]

	// Log the invocation
	if err := logInvocation(mockDir, command, args); err != nil {
		fmt.Fprintf(os.Stderr, "mockshim: failed to log invocation: %v\n", err)
		// Continue anyway - logging failure shouldn't break tests
	}

	// Load mock configuration
	mocks, err := loadConfig(mockDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mockshim: failed to load config: %v\n", err)

		return 255
	}

	// Find matching mock (first match wins - order matters for pattern matching)
	for _, mock := range mocks {
		if mock.Command == command && argsMatch(mock.Args, args) {
			// Handle passthrough - execute real command
			if mock.Passthrough {
				return executePassthrough(command, args)
			}

			// Output configured response
			if mock.Stdout != "" {
				_, _ = fmt.Fprint(os.Stdout, mock.Stdout)
			}

			if mock.Stderr != "" {
				_, _ = fmt.Fprint(os.Stderr, mock.Stderr)
			}

			return mock.ExitCode
		}
	}

	// No matching mock found
	fmt.Fprintf(os.Stderr, "mockshim: no mock configured for: %s %v\n", command, args)

	return 254
}

func loadConfig(mockDir string) ([]MockConfig, error) {
	configPath := filepath.Join(mockDir, "mock_config.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var mocks []MockConfig
	if err := json.Unmarshal(data, &mocks); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return mocks, nil
}

func logInvocation(mockDir, command string, args []string) error {
	invocation := Invocation{
		Command:   command,
		Args:      args,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(invocation)
	if err != nil {
		return fmt.Errorf("marshal invocation: %w", err)
	}

	// Append to invocation log file (unique per PID to support parallel execution)
	logPath := filepath.Join(mockDir, fmt.Sprintf("mock_invocations_%d.ndjson", os.Getpid()))

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write log: %w", err)
	}

	return nil
}

// argsMatch checks if actual arguments match a pattern.
// The pattern can contain special matcher strings (Any, AnyFurtherArgs).
// Mocks are matched in registration order, so more specific patterns should be registered first.
func argsMatch(pattern, actual []string) bool {
	patternIdx := 0
	actualIdx := 0

	for patternIdx < len(pattern) {
		if pattern[patternIdx] == matcherAnyFurtherArgs {
			// AnyFurtherArgs must be the last element in pattern
			// It matches zero or more remaining arguments
			return patternIdx == len(pattern)-1
		}

		if actualIdx >= len(actual) {
			return false // More pattern elements than actual args
		}

		if pattern[patternIdx] == matcherAny {
			// Any matches exactly one argument
			patternIdx++
			actualIdx++

			continue
		}

		// Exact match required
		if pattern[patternIdx] != actual[actualIdx] {
			return false
		}

		patternIdx++
		actualIdx++
	}

	// All pattern elements consumed; actual args must also be consumed
	return actualIdx == len(actual)
}

// executePassthrough executes the real command and passes through its output.
func executePassthrough(command string, args []string) int {
	originalPath := os.Getenv("GOCMDTESTER_ORIGINAL_PATH")
	if originalPath == "" {
		fmt.Fprintln(os.Stderr, "mockshim: GOCMDTESTER_ORIGINAL_PATH not set for passthrough")

		return 255
	}

	// Find the real binary in the original PATH
	realBinary := findInPath(command, originalPath)
	if realBinary == "" {
		fmt.Fprintf(os.Stderr, "mockshim: command not found in original PATH: %s\n", command)

		return 127 // Standard "command not found" exit code
	}

	// Execute the real command
	cmd := exec.Command(realBinary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	err := cmd.Run()
	if err != nil {
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}

		fmt.Fprintf(os.Stderr, "mockshim: failed to execute command: %v\n", err)

		return 1
	}

	return 0
}

// findInPath searches for a command in the given PATH string.
func findInPath(command, pathEnv string) string {
	for _, dir := range strings.Split(pathEnv, string(os.PathListSeparator)) {
		if dir == "" {
			continue
		}

		path := filepath.Join(dir, command)

		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}

	return ""
}
