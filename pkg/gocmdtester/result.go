// Package gocmdtester provides utilities for testing Go CLI binaries with coverage support.
package gocmdtester

import "strings"

// Result holds the output and exit code from running a command.
type Result struct {
	stdout   string
	stderr   string
	exitCode int
	err      error // Execution error (nil for successful execution)
}

// Stdout returns the stdout output from the command.
func (r *Result) Stdout() string {
	return r.stdout
}

// Stderr returns the stderr output from the command.
func (r *Result) Stderr() string {
	return r.stderr
}

// ExitCode returns the exit code from the command.
func (r *Result) ExitCode() int {
	return r.exitCode
}

// Err returns the execution error, if any.
// Returns nil if the binary executed successfully (regardless of exit code).
// Returns non-nil for execution failures (binary not found, permission denied, etc.).
func (r *Result) Err() error {
	return r.err
}

// Success returns true if the command executed successfully with exit code 0.
// This is a convenience method equivalent to: r.Err() == nil && r.ExitCode() == 0.
func (r *Result) Success() bool {
	return r.err == nil && r.exitCode == 0
}

// StdoutContains returns true if stdout contains the given substring.
func (r *Result) StdoutContains(s string) bool {
	return strings.Contains(r.stdout, s)
}

// StderrContains returns true if stderr contains the given substring.
func (r *Result) StderrContains(s string) bool {
	return strings.Contains(r.stderr, s)
}
