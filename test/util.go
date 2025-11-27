package test

import (
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/fsutil"
	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/dansimau/yas/pkg/xexec"
	"gotest.tools/v3/assert"
)

// mustExecOutput executes the specified command/args and returns the output
// from stdout. Panics if there is an error.
func mustExecOutput(workingDir string, args ...string) (output string) {
	b, err := xexec.Command(args...).WithEnvVars(gitexec.CleanedGitEnv()).WithWorkingDir(workingDir).Output()
	if err != nil {
		panic(err)
	}

	return string(b)
}

// mustExecExitCode executes the specified command/args and returns the exit code.
func mustExecExitCode(workingDir string, args ...string) int {
	err := xexec.Command(args...).WithEnvVars(gitexec.CleanedGitEnv()).WithWorkingDir(workingDir).Run()
	if err == nil {
		return 0
	}

	exitErr := &exec.ExitError{}
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}

	panic(err)
}

func mustMarshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	return string(b)
}

// equalLines asserts that both strings are equal after stripping
// leading/trailing whitespace.
func equalLines(t *testing.T, a, b string) {
	t.Helper()

	cleanedA := stripWhiteSpaceFromLines(a)
	cleanedB := stripWhiteSpaceFromLines(b)
	assert.Equal(t, cleanedA, cleanedB)
}

// stripWhiteSpaceFromLines strips leading and trailing whitespace from each
// line, and also from the overall string.
func stripWhiteSpaceFromLines(s string) string {
	lines := []string{}
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		lines = append(lines, strings.TrimSpace(line))
	}

	return strings.Join(lines, "\n")
}

// assertFileExists is a test helper that calls fsutil.FileExists and asserts
// that no error occurred, returning the boolean result.
func assertFileExists(t *testing.T, path string) bool {
	t.Helper()

	exists, err := fsutil.FileExists(path)
	assert.NilError(t, err, "FileExists should not error")

	return exists
}

// assertRestackStateExists is a test helper that checks if a restack state file exists.
func assertRestackStateExists(t *testing.T, repoDir string) bool {
	t.Helper()

	// Check both possible locations for the restack state file
	restackStateFiles := []string{".yas/yas.restack.json", ".git/.yasrestack"}
	for _, filename := range restackStateFiles {
		fullPath := repoDir + "/" + filename
		exists, err := fsutil.FileExists(fullPath)
		assert.NilError(t, err, "FileExists should not error")

		if exists {
			return true
		}
	}

	return false
}
