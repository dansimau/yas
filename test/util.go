package test

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/dansimau/yas/pkg/xexec"
	"gotest.tools/v3/assert"
)

func mustGetShortHash(ref string) string {
	if ref == "" {
		panic("ref is empty")
	}

	hash, err := gitexec.WithRepo(".").GetShortHash(ref)
	if err != nil {
		panic(err)
	}

	if hash == "" {
		panic("hash is empty")
	}

	return hash
}

func mustGetShortHashes(refs ...string) map[string]string {
	m := map[string]string{}

	for _, ref := range refs {
		m[ref] = mustGetShortHash(ref)
	}

	return m
}

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

// captureStdout captures stdout while executing the given function
func captureStdout(f func()) string {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}
