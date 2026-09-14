package tests

import (
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestGit_Passthrough(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	// Everything after the first non-option goes to git untouched.
	result := newCLI(t, filepath.Join(target, "sub")).Run("git", "--header", "log", "--oneline", "-1")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, strings.Count(result.Stdout(), "==>"), 4)
	assert.Equal(t, strings.Count(result.Stdout(), " initial"), 4)

	// Options workyard does not know are passed through too, even before the
	// git subcommand.
	result = newCLI(t, target).Run("git", "--header", "--no-pager", "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, strings.Count(result.Stdout(), "\nfeature\n"), 3)
	assert.Assert(t, cmp.Contains(result.Stdout(), "\nHEAD\n"))

	// "--" passes anything, including options workyard would otherwise take.
	result = newCLI(t, target).Run("git", "--", "--version")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, strings.Count(result.Stdout(), "git version"), 4)
}

func TestGit_ExitCodeReflectsFailures(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	// The branch "existing" only exists in repoA, so git fails in the others.
	result := newCLI(t, target).Run("git", "--header", "rev-parse", "--verify", "--quiet", "refs/heads/existing")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, cmp.Contains(result.Stderr(), "3 of 4 repositories"))
	assert.Assert(t, cmp.Contains(result.Stdout(), "==> repoA (feature)"))

	// stderr from git is forwarded.
	result = newCLI(t, target).Run("git", "log", "no-such-ref")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, cmp.Contains(result.Stderr(), "unknown revision"))
}

func TestGit_RequiresACommand(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	result := newCLI(t, target).Run("git")
	assert.Equal(t, result.ExitCode(), 2)
	assert.Assert(t, cmp.Contains(result.Stderr(), "no git command given"))
}

func TestGit_UnknownOptionOutsideFanOutIsAnError(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	result := newCLI(t, target).Run("list", "--bogus")
	assert.Equal(t, result.ExitCode(), 2)
	assert.Assert(t, cmp.Contains(result.Stderr(), "unknown flag"))
}
