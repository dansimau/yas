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

func TestStatus_FromSubdirectoryInPathOrder(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	// Run from a subdirectory that is not itself a repository.
	result := newCLI(t, filepath.Join(target, "plain", "nested")).Run("st")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())

	indexes := headerIndexes(t, result.Stdout(), "feature")
	for i := 1; i < len(indexes); i++ {
		assert.Assert(t, indexes[i-1] < indexes[i], "headers out of order:\n%s", result.Stdout())
	}

	// Default arguments are --short --branch.
	assert.Assert(t, cmp.Contains(result.Stdout(), "## feature"))
	assert.Assert(t, cmp.Contains(result.Stdout(), "## HEAD (no branch)"))
}

func TestStatus_ShowsChangesPerRepository(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	assert.NilError(t, os.WriteFile(filepath.Join(target, "repoA", "file.txt"), []byte("changed\n"), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(target, "wt", "main", "untracked.txt"), []byte("new\n"), 0o644))

	result := newCLI(t, target).Run("status")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())

	stdout := result.Stdout()
	repoA := between(t, stdout, "==> repoA", "==> sub/deep/repoB")
	assert.Assert(t, cmp.Contains(repoA, " M file.txt"))

	wtMain := between(t, stdout, "==> wt/main", "==> wt/other")
	assert.Assert(t, cmp.Contains(wtMain, "?? untracked.txt"))

	// Without --branch, clean repositories print nothing, so they get no
	// header either.
	result = newCLI(t, target).Run("st", "--short")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, strings.Count(result.Stdout(), "==>"), 2, result.Stdout())
	assert.Assert(t, cmp.Contains(result.Stdout(), "==> repoA (feature)"))
	assert.Assert(t, cmp.Contains(result.Stdout(), "==> wt/main (feature)"))
}

func TestStatus_ArgumentsReplaceDefaults(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	// --long is not a workyard option, so it is passed on to git status.
	result := newCLI(t, target).Run("st", "--long")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Assert(t, cmp.Contains(result.Stdout(), "On branch feature"))
	assert.Assert(t, !strings.Contains(result.Stdout(), "## feature"), "default --short --branch must be replaced")
}

func TestStatus_Unordered(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	result := newCLI(t, target).Run("--unordered", "st")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	headerIndexes(t, result.Stdout(), "feature")
}

func TestStatus_HeadersOnlyForRepositoriesWithOutput(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	// Nothing is modified, so "git status --short" prints nothing anywhere.
	result := newCLI(t, target).Run("st", "--short")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, result.Stdout(), "")

	// The default --branch output means every repository has a header.
	result = newCLI(t, target).Run("st")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, strings.Count(result.Stdout(), "==>"), 4)
	assert.Equal(t, strings.Count(result.Stdout(), "## "), 4)
}

func TestStatus_OutsideWorkyard(t *testing.T) {
	t.Parallel()

	outside := t.TempDir()

	result := newCLI(t, outside).Run("st")
	assert.Equal(t, result.ExitCode(), 2)
	assert.Assert(t, cmp.Contains(result.Stderr(), "not inside a workyard"))
	assert.Assert(t, cmp.Contains(result.Stderr(), "WORKYARD_ROOT"))

	// WORKYARD_ROOT makes any directory work.
	f := setupFixture(t)
	target := createYard(t, f)

	result = newCLI(t, outside, "WORKYARD_ROOT", target).Run("st")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	headerIndexes(t, result.Stdout(), "feature")
}

func TestStatus_MissingRepositoryIsReportedAndSkipped(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)
	assert.NilError(t, os.RemoveAll(filepath.Join(target, "sub", "deep", "repoB")))

	result := newCLI(t, target).Run("st")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Assert(t, cmp.Contains(result.Stderr(), "warning: sub/deep/repoB"))
	assert.Assert(t, cmp.Contains(result.Stderr(), "missing"))
	assert.Equal(t, strings.Count(result.Stdout(), "==>"), 3)
}
