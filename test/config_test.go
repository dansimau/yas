package test

import (
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yascli"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestMissingGitRepo(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		_, stderr, err := testutil.CaptureOutput(func() {
			exitCode := yascli.Run("list")
			assert.Equal(t, exitCode, 1)
		})

		assert.NilError(t, err)
		assert.Assert(t, cmp.Contains(stderr, "hint: specify --repo"))
	})
}

func TestNotInitialized(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `git init`)

		_, stderr, err := testutil.CaptureOutput(func() {
			exitCode := yascli.Run("list")
			assert.Equal(t, exitCode, 1)
		})

		assert.NilError(t, err)
		assert.Assert(t, cmp.Contains(stderr, "repository not configured"))
	})
}

func TestNotInitialized_Worktree(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			# Create main repo
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"

			# Create a worktree
			git worktree add worktree-dir
		`)

		_, stderr, err := testutil.CaptureOutput(func() {
			// Run from within the worktree (where .git is a file, not a directory)
			exitCode := yascli.Run("--repo=worktree-dir", "list")
			assert.Equal(t, exitCode, 1)
		})

		assert.NilError(t, err)
		assert.Assert(t, cmp.Contains(stderr, "repository not configured"),
			"Expected 'repository not configured' error in worktree, got: %s", stderr)
	})
}
