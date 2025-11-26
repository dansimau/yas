package test

import (
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"gotest.tools/v3/assert"
)

func TestMissingGitRepo(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Run list in a directory without a git repo
	result := cli.Run("list")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("hint: specify --repo"), "Expected hint about --repo in stderr, got: %s", result.Stderr())
}

func TestNotInitialized(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `git init`)

	result := cli.Run("list")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("repository not configured"), "Expected 'repository not configured' in stderr, got: %s", result.Stderr())
}

func TestNotInitialized_Worktree(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		# Create main repo
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"

		# Create a worktree
		git worktree add worktree-dir
	`)

	// Run from within the worktree (where .git is a file, not a directory)
	result := cli.Run("--repo=worktree-dir", "list")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("repository not configured"),
		"Expected 'repository not configured' error in worktree, got: %s", result.Stderr())
}
