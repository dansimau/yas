package test

import (
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"github.com/dansimau/yas/pkg/yascli"
	"gotest.tools/v3/assert"
)

func TestAbort_AbortsRebaseAndResetsBranch(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
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
		originalCommit := mustExecOutput("git", "rev-parse", "HEAD")
		originalCommit = strings.TrimSpace(originalCommit)

		// Initialize yas config
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "topic-a", "--parent=main"), 0)

		// Run restack - it should fail due to conflict
		exitCode := yascli.Run("restack")
		assert.Equal(t, exitCode, 1, "restack should fail due to conflict")

		// Verify that restack state was saved
		assert.Assert(t, yas.RestackStateExists("."), "restack state should be saved")

		// Run abort to cancel the restack
		exitCode = yascli.Run("abort")
		assert.Equal(t, exitCode, 0, "abort should succeed")

		// Verify that restack state was cleaned up
		assert.Assert(t, !yas.RestackStateExists("."), "restack state should be cleaned up")

		// Verify we're on topic-a
		equalLines(t, mustExecOutput("git", "branch", "--show-current"), "topic-a")

		// Verify topic-a was reset by git rebase --abort (should be back at original commit)
		currentCommit := strings.TrimSpace(mustExecOutput("git", "rev-parse", "HEAD"))
		assert.Equal(t, currentCommit, originalCommit, "branch should be back at original commit after rebase abort")

		// Verify topic-a still has old commits (not rebased)
		logOutput := mustExecOutput("git", "log", "--pretty=%s")
		assert.Assert(t, strings.Contains(logOutput, "topic-a-0"), "topic-a commit should exist")
		assert.Assert(t, strings.Contains(logOutput, "main-0"), "main-0 commit should exist")
		assert.Assert(t, !strings.Contains(logOutput, "main-1"), "main-1 should NOT be in history")
	})
}

func TestAbort_ReturnsToStartingBranch(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
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
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "topic-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "topic-b", "--parent=topic-a"), 0)

		// Verify we're on topic-b
		equalLines(t, mustExecOutput("git", "branch", "--show-current"), "topic-b")

		// Run restack - it should fail due to conflict on topic-a
		exitCode := yascli.Run("restack")
		assert.Equal(t, exitCode, 1, "restack should fail due to conflict")

		// During a rebase conflict, we're in detached HEAD state
		// The restack state should indicate we're rebasing topic-a
		assert.Assert(t, yas.RestackStateExists("."), "restack state should exist")
		state, err := yas.LoadRestackState(".")
		assert.NilError(t, err)
		assert.Equal(t, state.CurrentBranch, "topic-a", "state should show rebasing topic-a")
		assert.Equal(t, state.StartingBranch, "topic-b", "state should show starting from topic-b")

		// Run abort
		exitCode = yascli.Run("abort")
		assert.Equal(t, exitCode, 0, "abort should succeed")

		// Verify we're back on topic-b (the starting branch)
		currentBranch := strings.TrimSpace(mustExecOutput("git", "branch", "--show-current"))
		assert.Equal(t, currentBranch, "topic-b", "should return to starting branch after abort")
	})
}

func TestAbort_LeavesRebasedBranchesIntact(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
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
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "topic-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "topic-b", "--parent=topic-a"), 0)

		// Run restack - topic-a will succeed, topic-b will fail
		exitCode := yascli.Run("restack")
		assert.Equal(t, exitCode, 1, "restack should fail due to conflict on topic-b")

		// Verify restack state exists and shows the correct branch
		assert.Assert(t, yas.RestackStateExists("."), "restack state should exist")
		state, err := yas.LoadRestackState(".")
		assert.NilError(t, err)
		assert.Equal(t, state.CurrentBranch, "topic-b", "state should show rebasing topic-b")
		assert.Equal(t, len(state.RebasedBranches), 1, "should have 1 rebased branch")
		assert.Equal(t, state.RebasedBranches[0], "topic-a", "topic-a should be in rebased branches")

		// Run abort to clean up
		exitCode = yascli.Run("abort")
		assert.Equal(t, exitCode, 0, "abort should succeed")

		// Now we can check branches - verify topic-a is still rebased (abort doesn't undo it)
		testutil.ExecOrFail(t, "git checkout topic-a")

		logOutput := mustExecOutput("git", "log", "--pretty=%s")
		assert.Assert(t, strings.Contains(logOutput, "main-1"), "topic-a should still be rebased after abort")

		// Verify topic-b was reset (not rebased)
		testutil.ExecOrFail(t, "git checkout topic-b")

		logOutput = mustExecOutput("git", "log", "--pretty=%s")
		assert.Assert(t, !strings.Contains(logOutput, "main-1"), "topic-b should NOT include main-1 after abort")
	})
}

func TestAbort_NoRestackInProgress(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"
		`)

		// Initialize yas config
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)

		// Try to run abort with no restack in progress
		exitCode := yascli.Run("abort")
		assert.Equal(t, exitCode, 1, "abort should fail when no restack is in progress")
	})
}
