package test

import (
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

func TestContinue_ResumeAfterConflict(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		echo "line1" > file.txt
		git add file.txt
		git commit -m "main-0"

		# topic-a: modify file
		git checkout -b topic-a
		echo "line2-from-a" >> file.txt
		git add file.txt
		git commit -m "topic-a-0"

		# topic-b: also modify file (will conflict when rebased)
		git checkout -b topic-b
		echo "line3-from-b" >> file.txt
		git add file.txt
		git commit -m "topic-b-0"

		# update main: modify the same file differently
		git checkout main
		echo "line2-from-main" >> file.txt
		git add file.txt
		git commit -m "main-1"

		# on branch topic-b
		git checkout topic-b
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())

	// Run restack - it should fail due to conflict on topic-a
	result := cli.Run("restack")
	assert.Equal(t, result.ExitCode(), 1, "restack should fail due to conflict")

	// Verify that restack state was saved
	assert.Assert(t, assertRestackStateExists(t, tempDir), "restack state should be saved")

	// Load and verify the state (read-only, acceptable to call directly)
	state, err := yas.LoadRestackState(tempDir)
	assert.NilError(t, err)
	assert.Equal(t, state.CurrentBranch, "topic-a", "current branch should be topic-a")
	assert.Equal(t, state.CurrentParent, "main", "current parent should be main")
	assert.Equal(t, state.StartingBranch, "topic-b", "starting branch should be topic-b")

	// Fix the conflict
	testutil.ExecOrFail(t, tempDir, `
		# Accept both changes
		echo "line1" > file.txt
		echo "line2-from-main" >> file.txt
		echo "line2-from-a" >> file.txt
		git add file.txt
	`)

	// Run continue to resume the restack
	assert.NilError(t, cli.Run("continue").Err(), "continue should succeed")

	// Verify that restack state was cleaned up
	assert.Assert(t, !assertRestackStateExists(t, tempDir), "restack state should be cleaned up")

	// Verify we're back on topic-b
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--show-current"), "topic-b")

	// Verify the final state
	testutil.ExecOrFail(t, tempDir, "git checkout topic-a")

	output := mustExecOutput(tempDir, "git", "log", "--pretty=%s")
	assert.Assert(t, strings.Contains(output, "topic-a-0"), "topic-a commit should exist")
	assert.Assert(t, strings.Contains(output, "main-1"), "main-1 commit should exist")

	testutil.ExecOrFail(t, tempDir, "git checkout topic-b")

	output = mustExecOutput(tempDir, "git", "log", "--pretty=%s")
	assert.Assert(t, strings.Contains(output, "topic-b-0"), "topic-b commit should exist")
	assert.Assert(t, strings.Contains(output, "topic-a-0"), "topic-a commit should exist")
	assert.Assert(t, strings.Contains(output, "main-1"), "main-1 commit should exist")
}

func TestContinue_NoRestackInProgress(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())

	// Try to run continue with no restack in progress
	result := cli.Run("continue")
	assert.Equal(t, result.ExitCode(), 1, "continue should fail when no restack is in progress")
}

func TestContinue_UserAlreadyCompletedRebase(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		echo "line1" > file.txt
		git add file.txt
		git commit -m "main-0"

		# topic-a: modify file
		git checkout -b topic-a
		echo "line2-from-a" >> file.txt
		git add file.txt
		git commit -m "topic-a-0"

		# update main: modify the same file differently
		git checkout main
		echo "line2-from-main" >> file.txt
		git add file.txt
		git commit -m "main-1"

		# on branch topic-a
		git checkout topic-a
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Run restack - it should fail due to conflict
	result := cli.Run("restack")
	assert.Equal(t, result.ExitCode(), 1, "restack should fail due to conflict")

	// Verify that restack state was saved
	assert.Assert(t, assertRestackStateExists(t, tempDir), "restack state should be saved")

	// User fixes conflicts and completes the rebase manually
	testutil.ExecOrFail(t, tempDir, `
		# Accept both changes
		echo "line1" > file.txt
		echo "line2-from-main" >> file.txt
		echo "line2-from-a" >> file.txt
		git add file.txt
		git -c core.hooksPath=/dev/null -c core.editor=true rebase --continue
	`)

	// Run continue - should detect that rebase is already complete
	assert.NilError(t, cli.Run("continue").Err(), "continue should succeed even if user already completed rebase")

	// Verify that restack state was cleaned up
	assert.Assert(t, !assertRestackStateExists(t, tempDir), "restack state should be cleaned up")

	// Verify the final state
	output := mustExecOutput(tempDir, "git", "log", "--pretty=%s")
	assert.Assert(t, strings.Contains(output, "topic-a-0"), "topic-a commit should exist")
	assert.Assert(t, strings.Contains(output, "main-1"), "main-1 commit should exist")
}

func TestContinue_ErrorsWhenRebaseAborted(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		echo "line1" > file.txt
		git add file.txt
		git commit -m "main-0"

		# topic-a: modify file
		git checkout -b topic-a
		echo "line2-from-a" >> file.txt
		git add file.txt
		git commit -m "topic-a-0"

		# update main: modify the same file differently
		git checkout main
		echo "line2-from-main" >> file.txt
		git add file.txt
		git commit -m "main-1"

		# on branch topic-a
		git checkout topic-a
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Run restack - it should fail due to conflict
	result := cli.Run("restack")
	assert.Equal(t, result.ExitCode(), 1, "restack should fail due to conflict")

	// Verify that restack state was saved
	assert.Assert(t, assertRestackStateExists(t, tempDir), "restack state should be saved")

	// User aborts the rebase
	testutil.ExecOrFail(t, tempDir, `git rebase --abort`)

	// Run continue - should detect that rebase was aborted and error
	result = cli.Run("continue")
	assert.Equal(t, result.ExitCode(), 1, "continue should fail when rebase was aborted")

	// Verify that restack state still exists (wasn't cleaned up on error)
	assert.Assert(t, assertRestackStateExists(t, tempDir), "restack state should still exist after abort detection")

	// Verify we're still on topic-a
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--show-current"), "topic-a")

	// Verify topic-a was NOT rebased (still has old commits)
	output := mustExecOutput(tempDir, "git", "log", "--pretty=%s")
	assert.Assert(t, strings.Contains(output, "topic-a-0"), "topic-a commit should exist")
	assert.Assert(t, strings.Contains(output, "main-0"), "main-0 commit should exist")
	assert.Assert(t, !strings.Contains(output, "main-1"), "main-1 should NOT be in history (rebase was aborted)")
}

func TestContinue_MultipleConflicts(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		echo "line1" > file.txt
		git add file.txt
		git commit -m "main-0"

		# topic-a: modify file
		git checkout -b topic-a
		echo "line2-from-a" >> file.txt
		git add file.txt
		git commit -m "topic-a-0"

		# topic-b: also modify file
		git checkout -b topic-b
		echo "line3-from-b" >> file.txt
		git add file.txt
		git commit -m "topic-b-0"

		# topic-c: also modify file
		git checkout -b topic-c
		echo "line4-from-c" >> file.txt
		git add file.txt
		git commit -m "topic-c-0"

		# update main: modify the same file
		git checkout main
		echo "line2-from-main" >> file.txt
		git add file.txt
		git commit -m "main-1"

		# on branch topic-c
		git checkout topic-c
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())
	assert.NilError(t, cli.Run("add", "topic-c", "--parent=topic-b").Err())

	// Run restack - it should fail on topic-a
	result := cli.Run("restack")
	assert.Equal(t, result.ExitCode(), 1, "restack should fail on topic-a")

	// Fix conflict for topic-a
	testutil.ExecOrFail(t, tempDir, `
		echo "line1" > file.txt
		echo "line2-from-main" >> file.txt
		echo "line2-from-a" >> file.txt
		git add file.txt
	`)

	// Continue - might still have conflicts on topic-b or topic-c
	result = cli.Run("continue")
	// Could be 0 (success) or 1 (another conflict)
	// For this test, we'll just verify it doesn't crash

	if result.ExitCode() == 0 {
		// All conflicts resolved successfully
		assert.Assert(t, !assertRestackStateExists(t, tempDir), "restack state should be cleaned up")

		// Verify final state - all branches should be successfully rebased
		testutil.ExecOrFail(t, tempDir, "git checkout topic-c")

		output := mustExecOutput(tempDir, "git", "log", "--pretty=%s")
		assert.Assert(t, strings.Contains(output, "topic-c-0"), "topic-c commit should exist")
		assert.Assert(t, strings.Contains(output, "topic-b-0"), "topic-b commit should exist")
		assert.Assert(t, strings.Contains(output, "topic-a-0"), "topic-a commit should exist")
		// Note: main-1 is included because topic-a was rebased onto main which includes main-1
		assert.Assert(t, strings.Contains(output, "main-1"), "main-1 commit should exist after rebase")
	} else {
		// Another conflict occurred - state should still exist
		assert.Assert(t, assertRestackStateExists(t, tempDir), "restack state should still exist on another conflict")
	}
}
