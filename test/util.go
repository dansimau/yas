package test

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/fsutil"
	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/dansimau/yas/pkg/xexec"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

// mustExecOutput executes the specified command/args and returns the output
// from stdout. Panics if there is an error.
func mustExecOutput(args ...string) (output string) {
	b, err := xexec.Command(args...).WithEnvVars(gitexec.CleanedGitEnv()).Output()
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

// captureStdout captures stdout while executing the given function.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}

	return buf.String()
}

// assertFileExists is a test helper that calls fsutil.FileExists and asserts
// that no error occurred, returning the boolean result.
func assertFileExists(t *testing.T, path string) bool {
	t.Helper()

	exists, err := fsutil.FileExists(path)
	assert.NilError(t, err, "FileExists should not error")

	return exists
}

// assertRestackStateExists is a test helper that calls yas.RestackStateExists
// and asserts that no error occurred, returning the boolean result.
func assertRestackStateExists(t *testing.T, repoDir string) bool {
	t.Helper()

	exists, err := yas.RestackStateExists(repoDir)
	assert.NilError(t, err, "RestackStateExists should not error")

	return exists
}
