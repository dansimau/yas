package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestDiff_ShowsChangesInTheRightRepository(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	assert.NilError(t, os.WriteFile(filepath.Join(target, "repoA", "file.txt"), []byte("changed\n"), 0o644))

	result := newCLI(t, filepath.Join(target, "wt")).Run("diff")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())

	// Only the repository with a diff gets a header.
	stdout := result.Stdout()
	assert.Equal(t, strings.Count(stdout, "==>"), 1, stdout)

	repoA := between(t, stdout, "==> repoA (feature)", "")
	assert.Assert(t, cmp.Contains(repoA, "-content"))
	assert.Assert(t, cmp.Contains(repoA, "+changed"))

	// Options are passed to git diff.
	result = newCLI(t, target).Run("diff", "--stat")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, strings.Count(result.Stdout(), "==>"), 1)
	assert.Assert(t, cmp.Contains(result.Stdout(), "1 file changed"))
}

func TestDiff_NoChangesNoOutput(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	result := newCLI(t, target).Run("diff")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, result.Stdout(), "")
}

func TestDiff_GitDecidesColorItself(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)
	assert.NilError(t, os.WriteFile(filepath.Join(target, "repoA", "file.txt"), []byte("changed\n"), 0o644))

	// workyard does not touch git's color settings: git sees a pipe here and
	// stays plain, and the user's own options are passed through.
	result := newCLI(t, target).Run("diff")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Assert(t, !strings.Contains(result.Stdout(), "\x1b["), "no escape codes when stdout is not a terminal")

	result = newCLI(t, target).Run("diff", "--color=always")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Assert(t, cmp.Contains(result.Stdout(), "\x1b["))
}
