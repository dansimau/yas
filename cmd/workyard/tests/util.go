package tests

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/workyard"
	"github.com/dansimau/yas/pkg/xexec"
	"gotest.tools/v3/assert"
)

const mainGo = "../main.go"

// copyModes are the copy modes every create test runs under. On macOS "auto"
// exercises clonefile (source and target share the temp volume); elsewhere
// both modes copy.
var copyModes = []string{"auto", "plain"}

// fixtureRepos are the repositories in the standard fixture, in path order.
var fixtureRepos = []string{"repoA", "sub/deep/repoB", "wt/main", "wt/other"}

// newCLI returns a tester running the workyard binary in workingDir with the
// given KEY, VALUE environment pairs.
func newCLI(t *testing.T, workingDir string, env ...string) *gocmdtester.CmdTester {
	t.Helper()

	opts := []gocmdtester.Option{gocmdtester.WithWorkingDir(workingDir)}
	for i := 0; i+1 < len(env); i += 2 {
		opts = append(opts, gocmdtester.WithEnv(env[i], env[i+1]))
	}

	return gocmdtester.FromPath(t, mainGo, opts...)
}

// mustOutput runs a command and returns its trimmed stdout.
func mustOutput(t *testing.T, workingDir string, args ...string) string {
	t.Helper()

	b, err := xexec.Command(args...).
		WithEnvVars(gitexec.CleanedGitEnv()).
		WithWorkingDir(workingDir).
		WithStdout(nil).
		Output()
	assert.NilError(t, err, "%v", args)

	return strings.TrimSpace(string(b))
}

// between returns the part of s from the start marker up to the end marker
// (or the end of s when end is ""), failing the test if a marker is missing.
func between(t *testing.T, s string, start string, end string) string {
	t.Helper()

	from := strings.Index(s, start)
	assert.Assert(t, from >= 0, "missing %q in:\n%s", start, s)

	if end == "" {
		return s[from:]
	}

	to := strings.Index(s, end)
	assert.Assert(t, to >= from, "missing %q after %q in:\n%s", end, start, s)

	return s[from:to]
}

// initRepo creates a repository at path with one commit on main and runs the
// extra shell lines in it afterwards.
func initRepo(t *testing.T, path string, extra string) {
	t.Helper()

	assert.NilError(t, os.MkdirAll(path, 0o755))
	testutil.ExecOrFail(t, path, `
		git init -q --initial-branch=main
		echo content > file.txt
		git add file.txt
		git commit -q -m initial
	`+extra)
}

// currentBranch returns the branch checked out at dir, or "" when detached.
func currentBranch(t *testing.T, dir string) string {
	t.Helper()

	b, err := xexec.Command("git", "symbolic-ref", "--short", "-q", "HEAD").
		WithEnvVars(gitexec.CleanedGitEnv()).
		WithWorkingDir(dir).
		WithStdout(nil).
		WithStderr(nil).
		Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(b))
}

func headHash(t *testing.T, dir string, ref string) string {
	t.Helper()

	return mustOutput(t, dir, "git", "rev-parse", ref)
}

// worktreeCount returns the number of worktrees (including the primary one)
// git knows about for the repository at dir.
func worktreeCount(t *testing.T, dir string) int {
	t.Helper()

	return len(strings.Split(mustOutput(t, dir, "git", "worktree", "list"), "\n"))
}

func openYard(t *testing.T, root string) *workyard.Yard {
	t.Helper()

	yard, err := workyard.Open(root)
	assert.NilError(t, err)

	return yard
}

func assertFileContent(t *testing.T, path string, expected string) {
	t.Helper()

	b, err := os.ReadFile(path)
	assert.NilError(t, err)
	assert.Equal(t, string(b), expected)
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()

	_, err := os.Lstat(path)
	assert.Assert(t, errors.Is(err, os.ErrNotExist), "%s should not exist (err: %v)", path, err)
}

func assertExists(t *testing.T, path string) {
	t.Helper()

	_, err := os.Lstat(path)
	assert.NilError(t, err, "%s should exist", path)
}

// allowCleanup makes a read-only directory writable again before the temp
// directory is removed.
func allowCleanup(t *testing.T, dir string) {
	t.Helper()

	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o755)
	})
}

// fixture is the standard source tree: plain files, a symlink, a nested
// directory, a read-only directory and four repositories (two plain ones, and
// a primary worktree plus a linked worktree that share a git directory).
type fixture struct {
	Source string
}

func setupFixture(t *testing.T) fixture {
	t.Helper()

	source := filepath.Join(t.TempDir(), "src")
	assert.NilError(t, os.MkdirAll(filepath.Join(source, "plain", "nested"), 0o755))
	assert.NilError(t, os.MkdirAll(filepath.Join(source, "readonly"), 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(source, "top.txt"), []byte("hello\n"), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(source, "exec.sh"), []byte("#!/bin/sh\n"), 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(source, "plain", "nested", "n.txt"), []byte("nested\n"), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(source, "readonly", "inside.txt"), []byte("ro\n"), 0o644))
	assert.NilError(t, os.Symlink("top.txt", filepath.Join(source, "link")))
	assert.NilError(t, os.Chmod(filepath.Join(source, "readonly"), 0o555))
	allowCleanup(t, filepath.Join(source, "readonly"))

	initRepo(t, filepath.Join(source, "repoA"), `
		git branch existing
	`)
	initRepo(t, filepath.Join(source, "sub", "deep", "repoB"), "")
	initRepo(t, filepath.Join(source, "wt", "main"), `
		git worktree add -q -b other ../other
	`)

	return fixture{Source: source}
}

// create runs "workyard create" for the fixture and returns the target path
// and the result.
func (f fixture) create(t *testing.T, mode string, args ...string) (string, *gocmdtester.Result) {
	t.Helper()

	target := filepath.Join(t.TempDir(), "yard")
	allowCleanup(t, filepath.Join(target, "readonly"))

	cli := newCLI(t, f.Source, "WORKYARD_COPY_MODE", mode)
	result := cli.Run(append([]string{"create", "--source", f.Source, "--branch", "feature", target}, args...)...)

	return target, result
}

// createYard creates a yard from the fixture and fails the test if that does
// not work.
func createYard(t *testing.T, f fixture, args ...string) string {
	t.Helper()

	target, result := f.create(t, "auto", args...)
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())

	return target
}
