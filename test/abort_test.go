package test

import (
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

func TestAbort_AbortsRebaseAndResetsBranch(t *testing.T) {
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

	// Capture the original commit of topic-a
	originalCommit := mustExecOutput(tempDir, "git", "rev-parse", "HEAD")
	originalCommit = strings.TrimSpace(originalCommit)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Run restack - it should fail due to conflict
	result := cli.Run("restack")
	assert.Equal(t, result.ExitCode(), 1, "restack should fail due to conflict")

	// Verify that restack state was saved
	assert.Assert(t, assertRestackStateExists(t, tempDir), "restack state should be saved")

	// Run abort to cancel the restack
	assert.NilError(t, cli.Run("abort").Err(), "abort should succeed")

	// Verify that restack state was cleaned up
	assert.Assert(t, !assertRestackStateExists(t, tempDir), "restack state should be cleaned up")

	// Verify we're on topic-a
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--show-current"), "topic-a")

	// Verify topic-a was reset by git rebase --abort (should be back at original commit)
	currentCommit := strings.TrimSpace(mustExecOutput(tempDir, "git", "rev-parse", "HEAD"))
	assert.Equal(t, currentCommit, originalCommit, "branch should be back at original commit after rebase abort")

	// Verify topic-a still has old commits (not rebased)
	logOutput := mustExecOutput(tempDir, "git", "log", "--pretty=%s")
	assert.Assert(t, strings.Contains(logOutput, "topic-a-0"), "topic-a commit should exist")
	assert.Assert(t, strings.Contains(logOutput, "main-0"), "main-0 commit should exist")
	assert.Assert(t, !strings.Contains(logOutput, "main-1"), "main-1 should NOT be in history")
}

func TestAbort_ReturnsToStartingBranch(t *testing.T) {
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

		# topic-b: branch from topic-a
		git checkout -b topic-b
		echo "line3" >> file.txt
		git add file.txt
		git commit -m "topic-b-0"

		# update main: modify the same file differently
		git checkout main
		echo "line2-from-main" >> file.txt
		git add file.txt
		git commit -m "main-1"

		# Start from topic-b
		git checkout topic-b
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())

	// Verify we're on topic-b
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--show-current"), "topic-b")

	// Run restack --all - it should fail due to conflict on topic-a
	// (using --all to restack all branches, not just current branch and descendants)
	result := cli.Run("restack", "--all")
	assert.Equal(t, result.ExitCode(), 1, "restack should fail due to conflict")

	// During a rebase conflict, we're in detached HEAD state
	// The restack state should indicate we're rebasing topic-a
	assert.Assert(t, assertRestackStateExists(t, tempDir), "restack state should exist")

	state, err := yas.LoadRestackState(tempDir)
	assert.NilError(t, err)
	assert.Equal(t, state.CurrentBranch, "topic-a", "state should show rebasing topic-a")
	assert.Equal(t, state.StartingBranch, "topic-b", "state should show starting from topic-b")

	// Run abort
	assert.NilError(t, cli.Run("abort").Err(), "abort should succeed")

	// Verify we're back on topic-b (the starting branch)
	currentBranch := strings.TrimSpace(mustExecOutput(tempDir, "git", "branch", "--show-current"))
	assert.Equal(t, currentBranch, "topic-b", "should return to starting branch after abort")
}

func TestAbort_LeavesRebasedBranchesIntact(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		touch file.txt
		git add file.txt
		git commit -m "main-0"

		# topic-a
		git checkout -b topic-a
		echo "a" > a.txt
		git add a.txt
		git commit -m "topic-a-0"

		# topic-b: will conflict
		git checkout -b topic-b
		echo "line1" >> file.txt
		git add file.txt
		git commit -m "topic-b-0"

		# update main
		git checkout main
		echo "updated" >> file.txt
		git add file.txt
		git commit -m "main-1"

		# Start from topic-b
		git checkout topic-b
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())

	// Run restack --all - topic-a will succeed, topic-b will fail
	// (using --all to restack all branches, not just current branch and descendants)
	result := cli.Run("restack", "--all")
	assert.Equal(t, result.ExitCode(), 1, "restack should fail due to conflict on topic-b")

	// Verify restack state exists and shows the correct branch
	assert.Assert(t, assertRestackStateExists(t, tempDir), "restack state should exist")

	state, err := yas.LoadRestackState(tempDir)
	assert.NilError(t, err)
	assert.Equal(t, state.CurrentBranch, "topic-b", "state should show rebasing topic-b")
	assert.Equal(t, len(state.RebasedBranches), 1, "should have 1 rebased branch")
	assert.Equal(t, state.RebasedBranches[0], "topic-a", "topic-a should be in rebased branches")

	// Run abort to clean up
	assert.NilError(t, cli.Run("abort").Err(), "abort should succeed")

	// Now we can check branches - verify topic-a is still rebased (abort doesn't undo it)
	testutil.ExecOrFail(t, tempDir, "git checkout topic-a")

	logOutput := mustExecOutput(tempDir, "git", "log", "--pretty=%s")
	assert.Assert(t, strings.Contains(logOutput, "main-1"), "topic-a should still be rebased after abort")

	// Verify topic-b was reset (not rebased)
	testutil.ExecOrFail(t, tempDir, "git checkout topic-b")

	logOutput = mustExecOutput(tempDir, "git", "log", "--pretty=%s")
	assert.Assert(t, !strings.Contains(logOutput, "main-1"), "topic-b should NOT include main-1 after abort")
}

func TestAbort_NoRestackInProgress(t *testing.T) {
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

	// Try to run abort with no restack in progress
	result := cli.Run("abort")
	assert.Equal(t, result.ExitCode(), 1, "abort should fail when no restack is in progress")
}
