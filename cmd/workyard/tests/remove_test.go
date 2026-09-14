package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestRemove_CleanYard(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	result := newCLI(t, t.TempDir()).Run("remove", target)
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assertNotExists(t, target)

	// Worktrees are gone from git's point of view too.
	for _, repo := range []string{"repoA", "sub/deep/repoB"} {
		assert.Equal(t, worktreeCount(t, filepath.Join(f.Source, repo)), 1, repo)
	}

	assert.Equal(t, worktreeCount(t, filepath.Join(f.Source, "wt", "main")), 2, "main and other remain")

	// Branches created by workyard are kept without --delete-branch.
	assert.Equal(t, mustOutput(t, filepath.Join(f.Source, "repoA"), "git", "branch", "--list", "feature"), "feature")
}

func TestRemove_DirtyYardNeedsForce(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)
	assert.NilError(t, os.WriteFile(filepath.Join(target, "repoA", "file.txt"), []byte("changed\n"), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(target, "wt", "other", "new.txt"), []byte("new\n"), 0o644))

	result := newCLI(t, t.TempDir()).Run("remove", target)
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, cmp.Contains(result.Stderr(), "uncommitted changes"))
	assert.Assert(t, cmp.Contains(result.Stderr(), "repoA"))
	assert.Assert(t, cmp.Contains(result.Stderr(), "wt/other"))
	assert.Assert(t, cmp.Contains(result.Stderr(), "--force"))

	// Nothing was removed, not even the clean worktrees.
	assertExists(t, filepath.Join(target, "sub", "deep", "repoB", ".git"))
	assert.Equal(t, worktreeCount(t, filepath.Join(f.Source, "sub", "deep", "repoB")), 2)

	result = newCLI(t, t.TempDir()).Run("remove", "-f", target)
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assertNotExists(t, target)
	assert.Equal(t, worktreeCount(t, filepath.Join(f.Source, "repoA")), 1)
}

func TestRemove_LockedWorktreeNeedsForceTwice(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)
	assert.NilError(t, os.WriteFile(filepath.Join(target, "repoA", "file.txt"), []byte("changed\n"), 0o644))
	mustOutput(t, filepath.Join(f.Source, "repoA"), "git", "worktree", "lock", filepath.Join(target, "repoA"))

	result := newCLI(t, t.TempDir()).Run("remove", "-f", target)
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, cmp.Contains(result.Stderr(), "locked"))
	assertExists(t, target)

	result = newCLI(t, t.TempDir()).Run("remove", "-f", "-f", target)
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assertNotExists(t, target)
}

func TestRemove_DeletedSourceFallsBackToRemoveAll(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)
	assert.NilError(t, os.RemoveAll(filepath.Join(f.Source, "sub", "deep", "repoB")))

	result := newCLI(t, t.TempDir()).Run("remove", target)
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Assert(t, cmp.Contains(result.Stderr(), "warning: sub/deep/repoB"))
	assert.Assert(t, cmp.Contains(result.Stderr(), "no longer exists"))
	assertNotExists(t, target)
	assert.Equal(t, worktreeCount(t, filepath.Join(f.Source, "repoA")), 1)
}

func TestRemove_DeleteBranchOnlyDeletesCreatedBranches(t *testing.T) {
	t.Parallel()

	// "existing" already exists in repoA, so it is checked out there and
	// created everywhere else.
	f := setupFixture(t)
	target := createYard(t, f, "--branch", "existing")

	yard := openYard(t, target)
	assert.Assert(t, !yard.Meta.Repos[0].CreatedBranch, "repoA already had the branch")
	assert.Assert(t, yard.Meta.Repos[1].CreatedBranch)

	result := newCLI(t, t.TempDir()).Run("remove", "--delete-branch", target)
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assertNotExists(t, target)

	assert.Equal(t, mustOutput(t, filepath.Join(f.Source, "repoA"), "git", "branch", "--list", "existing"), "existing")
	assert.Equal(t, mustOutput(t, filepath.Join(f.Source, "sub", "deep", "repoB"), "git", "branch", "--list", "existing"), "")
	assert.Equal(t, mustOutput(t, filepath.Join(f.Source, "wt", "main"), "git", "branch", "--list", "existing"), "")
}

func TestRemove_DeleteBranchRefusesUnmergedWithoutForce(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)
	mustOutput(t, filepath.Join(target, "repoA"), "git", "commit", "-q", "--allow-empty", "-m", "unmerged work")

	result := newCLI(t, t.TempDir()).Run("remove", "--delete-branch", target)
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, cmp.Contains(result.Stderr(), "delete branch feature"))
	assertNotExists(t, target) // the yard itself is removed; only the branch deletion failed
	assert.Equal(t, mustOutput(t, filepath.Join(f.Source, "repoA"), "git", "branch", "--list", "feature"), "feature")

	// The other branches, which had no extra commits, were deleted.
	assert.Equal(t, mustOutput(t, filepath.Join(f.Source, "sub", "deep", "repoB"), "git", "branch", "--list", "feature"), "")
}

func TestRemove_WithoutPathNeedsConfirmationOrYes(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)
	outside := t.TempDir()

	// Declining the prompt aborts.
	cli := gocmdtester.FromPath(t, mainGo,
		gocmdtester.WithWorkingDir(outside),
		gocmdtester.WithEnv("WORKYARD_ROOT", target),
		gocmdtester.WithStdin(strings.NewReader("n\n")),
	)
	result := cli.Run("remove")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, cmp.Contains(result.Stderr(), "Remove workyard"))
	assert.Assert(t, cmp.Contains(result.Stderr(), "aborted"))
	assertExists(t, target)

	// Running from inside the yard is refused, --yes or not.
	result = newCLI(t, filepath.Join(target, "plain")).Run("remove", "--yes")
	assert.Equal(t, result.ExitCode(), 2)
	assert.Assert(t, cmp.Contains(result.Stderr(), "inside"))
	assertExists(t, target)

	// Accepting the prompt removes it.
	cli = gocmdtester.FromPath(t, mainGo,
		gocmdtester.WithWorkingDir(outside),
		gocmdtester.WithEnv("WORKYARD_ROOT", target),
		gocmdtester.WithStdin(strings.NewReader("y\n")),
	)
	result = cli.Run("remove")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assertNotExists(t, target)
}

func TestRemove_YesSkipsConfirmation(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	result := newCLI(t, t.TempDir(), "WORKYARD_ROOT", target).Run("remove", "--yes")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assertNotExists(t, target)
}

func TestRemove_NotAWorkyard(t *testing.T) {
	t.Parallel()

	result := newCLI(t, t.TempDir()).Run("remove", t.TempDir())
	assert.Equal(t, result.ExitCode(), 2)
	assert.Assert(t, cmp.Contains(result.Stderr(), "not inside a workyard"))

	// Options may follow the path.
	result = newCLI(t, t.TempDir()).Run("remove", "some/path", "--yes")
	assert.Equal(t, result.ExitCode(), 2)
	assert.Assert(t, cmp.Contains(result.Stderr(), "not inside a workyard"))

	result = newCLI(t, t.TempDir()).Run("remove", "some/path", "another")
	assert.Equal(t, result.ExitCode(), 2)
	assert.Assert(t, cmp.Contains(result.Stderr(), "unexpected arguments"))
}
