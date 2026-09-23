package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

// headerIndexes returns the offsets of each repository's header in stdout,
// in fixture order, failing if any is missing.
func headerIndexes(t *testing.T, stdout string, branch string) []int {
	t.Helper()

	var indexes []int

	for _, repo := range fixtureRepos {
		header := "==> " + repo + " (" + branch + ")"
		if repo == "wt/other" {
			header = "==> " + repo + " (HEAD)"
		}

		index := strings.Index(stdout, header)
		assert.Assert(t, index >= 0, "missing header %q in:\n%s", header, stdout)

		indexes = append(indexes, index)
	}

	return indexes
}

func TestGit_FromSubdirectoryInPathOrder(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	// Run from a subdirectory that is not itself a repository.
	result := newCLI(t, filepath.Join(target, "plain", "nested")).Run("git", "status", "--short", "--branch")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())

	indexes := headerIndexes(t, result.Stdout(), "feature")
	for i := 1; i < len(indexes); i++ {
		assert.Assert(t, indexes[i-1] < indexes[i], "headers out of order:\n%s", result.Stdout())
	}

	assert.Assert(t, cmp.Contains(result.Stdout(), "## feature"))
	assert.Assert(t, cmp.Contains(result.Stdout(), "## HEAD (no branch)"))
}

func TestGit_ArgumentsArePassedVerbatim(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	assert.NilError(t, os.WriteFile(filepath.Join(target, "repoA", "file.txt"), []byte("changed\n"), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(target, "wt", "main", "untracked.txt"), []byte("new\n"), 0o644))

	// Nothing is added: plain "git status" prints its long format.
	result := newCLI(t, target).Run("git", "status")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, strings.Count(result.Stdout(), "==>"), 4)
	assert.Equal(t, strings.Count(result.Stdout(), "On branch feature"), 3)

	repoA := between(t, result.Stdout(), "==> repoA", "==> sub/deep/repoB")
	assert.Assert(t, cmp.Contains(repoA, "modified:   file.txt"))

	wtMain := between(t, result.Stdout(), "==> wt/main", "==> wt/other")
	assert.Assert(t, cmp.Contains(wtMain, "untracked.txt"))

	// Options before the subcommand, which workyard does not know, go to git
	// too.
	result = newCLI(t, target).Run("git", "--no-pager", "diff", "--stat")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Assert(t, cmp.Contains(between(t, result.Stdout(), "==> repoA", "==> sub/deep/repoB"), "1 file changed"))

	// git aliases work as they would in the repository.
	result = newCLI(t, target).Run("git", "-c", "alias.st=status --short", "st")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Assert(t, cmp.Contains(result.Stdout(), " M file.txt"))
	assert.Assert(t, cmp.Contains(result.Stdout(), "?? untracked.txt"))
}

func TestGit_Parallel(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)
	assert.NilError(t, os.WriteFile(filepath.Join(target, "repoA", "file.txt"), []byte("changed\n"), 0o644))

	// Captured output: only the repository with a diff gets a header.
	result := newCLI(t, filepath.Join(target, "wt")).Run("git", "--parallel", "diff")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, strings.Count(result.Stdout(), "==>"), 1, result.Stdout())

	repoA := between(t, result.Stdout(), "==> repoA (feature)", "")
	assert.Assert(t, cmp.Contains(repoA, "-content"))
	assert.Assert(t, cmp.Contains(repoA, "+changed"))

	result = newCLI(t, target).Run("--unordered", "git", "--parallel", "status", "--short", "--branch")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	headerIndexes(t, result.Stdout(), "feature")

	result = newCLI(t, target).Run("git", "--parallel", "--no-parallel", "status")
	assert.Equal(t, result.ExitCode(), 2)
	assert.Assert(t, cmp.Contains(result.Stderr(), "cannot be combined"))
}

func TestGit_ConfigSetsTheDefaultMode(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	// Serial by default: a header for every repository, even without output.
	result := newCLI(t, target).Run("git", "diff")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, strings.Count(result.Stdout(), "==>"), 4)

	assert.NilError(t, os.WriteFile(filepath.Join(f.Source, ".workyard", "config.yaml"), []byte("exec:\n  parallel: true\n"), 0o644))

	result = newCLI(t, target).Run("git", "diff")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, result.Stdout(), "", "parallel mode prints no header without output")

	result = newCLI(t, target).Run("git", "--no-parallel", "diff")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, strings.Count(result.Stdout(), "==>"), 4)
}

func TestGit_GitDecidesColorItself(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)
	assert.NilError(t, os.WriteFile(filepath.Join(target, "repoA", "file.txt"), []byte("changed\n"), 0o644))

	// workyard does not touch git's color settings: git sees a pipe here and
	// stays plain, and the user's own options are passed through.
	result := newCLI(t, target).Run("git", "diff")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Assert(t, !strings.Contains(result.Stdout(), "\x1b["), "no escape codes when stdout is not a terminal")

	result = newCLI(t, target).Run("git", "diff", "--color=always")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Assert(t, cmp.Contains(result.Stdout(), "\x1b["))
}

func TestGit_OutsideWorkyard(t *testing.T) {
	t.Parallel()

	outside := t.TempDir()

	result := newCLI(t, outside).Run("git", "status")
	assert.Equal(t, result.ExitCode(), 2)
	assert.Assert(t, cmp.Contains(result.Stderr(), "not inside a workyard"))
	assert.Assert(t, cmp.Contains(result.Stderr(), "WORKYARD_ROOT"))

	// WORKYARD_ROOT makes any directory work.
	f := setupFixture(t)
	target := createYard(t, f)

	result = newCLI(t, outside, "WORKYARD_ROOT", target).Run("git", "status")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	headerIndexes(t, result.Stdout(), "feature")
}

func TestGit_FailuresAreReported(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)
	assert.NilError(t, os.RemoveAll(filepath.Join(target, "sub", "deep", "repoB")))

	// The other repositories still run; the missing one is a failure.
	for _, mode := range []string{"--parallel", "--no-parallel"} {
		result := newCLI(t, target).Run("git", mode, "status", "--short", "--branch")
		assert.Equal(t, result.ExitCode(), 1, mode)
		assert.Assert(t, cmp.Contains(result.Stderr(), "warning: sub/deep/repoB"), mode)
		assert.Assert(t, cmp.Contains(result.Stderr(), "missing"), mode)
		assert.Assert(t, cmp.Contains(result.Stderr(), "git failed in 1 of 4 repositories"), mode)
		assert.Equal(t, strings.Count(result.Stdout(), "## "), 3, mode)
	}
}
