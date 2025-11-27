package gocmdtester

import "time"

// Mock represents a mock for a command execution.
// Mocks are created via CmdTester.Mock() and configured using builder methods.
//
// Example:
//
//	mock := tester.Mock("gh", "pr", "create").
//	    WithStdout("https://github.com/user/repo/pull/1").
//	    WithCode(0)
//
//	result := tester.Run(t, "submit")
//
//	if !mock.Called() {
//	    t.Error("expected gh pr create to be called")
//	}
type Mock struct {
	command     string
	args        []string
	stdout      string
	stderr      string
	exitCode    int
	passthrough bool
	registry    *mockRegistry
}

// WithCode sets the exit code to return when this mock is invoked.
func (m *Mock) WithCode(code int) *Mock {
	m.exitCode = code

	return m
}

// WithStdout sets the stdout output to return when this mock is invoked.
func (m *Mock) WithStdout(s string) *Mock {
	m.stdout = s

	return m
}

// WithStderr sets the stderr output to return when this mock is invoked.
func (m *Mock) WithStderr(s string) *Mock {
	m.stderr = s

	return m
}

// WithPassthroughExec configures this mock to execute the real command
// instead of returning mock data. The real command's stdout, stderr,
// and exit code are passed through to the caller.
func (m *Mock) WithPassthroughExec() *Mock {
	m.passthrough = true

	return m
}

// Called returns true if this mock was invoked at least once.
func (m *Mock) Called() bool {
	return m.CalledTimes() > 0
}

// CalledTimes returns the number of times this mock was invoked.
func (m *Mock) CalledTimes() int {
	return len(m.Calls())
}

// CalledWithArgs returns true if this mock was invoked with the given arguments.
// The args parameter should be the actual arguments (not patterns).
// For pattern-based mocks, use this to verify specific invocations.
func (m *Mock) CalledWithArgs(args ...string) bool {
	calls := m.Calls()
	for _, call := range calls {
		if argsEqual(call.Args, args) {
			return true
		}
	}

	return false
}

// Calls returns all invocations that match this mock.
// For pattern-based mocks (using Any or AnyFurtherArgs), this returns
// all invocations that match the pattern.
func (m *Mock) Calls() []Invocation {
	if m.registry == nil {
		return nil
	}

	invocations, err := m.registry.loadInvocations()
	if err != nil {
		return nil
	}

	var matches []Invocation

	for _, inv := range invocations {
		if inv.Command == m.command && argsMatch(m.args, inv.Args) {
			matches = append(matches, inv)
		}
	}

	return matches
}

// Invocation represents a recorded command invocation.
type Invocation struct {
	Command   string    `json:"command"`
	Args      []string  `json:"args"`
	Timestamp time.Time `json:"timestamp"`
}

// argsEqual compares two string slices for equality.
func argsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

// mockConfig represents a mock configuration for serialization.
type mockConfig struct {
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	Stdout      string   `json:"stdout"`
	Stderr      string   `json:"stderr"`
	ExitCode    int      `json:"exitCode"`
	Passthrough bool     `json:"passthrough"`
}

// toConfig converts a Mock to its serializable configuration.
func (m *Mock) toConfig() mockConfig {
	return mockConfig{
		Command:     m.command,
		Args:        m.args,
		Stdout:      m.stdout,
		Stderr:      m.stderr,
		ExitCode:    m.exitCode,
		Passthrough: m.passthrough,
	}
}
