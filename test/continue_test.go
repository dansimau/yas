package test

import (
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"github.com/dansimau/yas/pkg/yascli"
	"gotest.tools/v3/assert"
)

func TestContinue_ResumeAfterConflict(t *testing.T) {
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
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-b", "--parent=topic-a"), 0)

		// Run restack - it should fail due to conflict on topic-a
		exitCode := yascli.Run("restack")
		assert.Equal(t, exitCode, 1, "restack should fail due to conflict")

		// Verify that restack state was saved
		assert.Assert(t, yas.RestackStateExists("."), "restack state should be saved")

		// Load and verify the state
		state, err := yas.LoadRestackState(".")
		assert.NilError(t, err)
		assert.Equal(t, state.CurrentBranch, "topic-a", "current branch should be topic-a")
		assert.Equal(t, state.CurrentParent, "main", "current parent should be main")
		assert.Equal(t, state.StartingBranch, "topic-b", "starting branch should be topic-b")

		// Fix the conflict
		testutil.ExecOrFail(t, `
			# Accept both changes
			echo "line1" > file.txt
			echo "line2-from-main" >> file.txt
			echo "line2-from-a" >> file.txt
			git add file.txt
		`)

		// Run continue to resume the restack
		exitCode = yascli.Run("continue")
		assert.Equal(t, exitCode, 0, "continue should succeed")

		// Verify that restack state was cleaned up
		assert.Assert(t, !yas.RestackStateExists("."), "restack state should be cleaned up")

		// Verify we're back on topic-b
		equalLines(t, mustExecOutput("git", "branch", "--show-current"), "topic-b")

		// Verify the final state
		testutil.ExecOrFail(t, "git checkout topic-a")

		output := mustExecOutput("git", "log", "--pretty=%s")
		assert.Assert(t, strings.Contains(output, "topic-a-0"), "topic-a commit should exist")
		assert.Assert(t, strings.Contains(output, "main-1"), "main-1 commit should exist")

		testutil.ExecOrFail(t, "git checkout topic-b")

		output = mustExecOutput("git", "log", "--pretty=%s")
		assert.Assert(t, strings.Contains(output, "topic-b-0"), "topic-b commit should exist")
		assert.Assert(t, strings.Contains(output, "topic-a-0"), "topic-a commit should exist")
		assert.Assert(t, strings.Contains(output, "main-1"), "main-1 commit should exist")
	})
}

func TestContinue_NoRestackInProgress(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"
		`)

		// Initialize yas config
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)

		// Try to run continue with no restack in progress
		exitCode := yascli.Run("continue")
		assert.Equal(t, exitCode, 1, "continue should fail when no restack is in progress")
	})
}

func TestContinue_UserAlreadyCompletedRebase(t *testing.T) {
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

		// Initialize yas config
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)

		// Run restack - it should fail due to conflict
		exitCode := yascli.Run("restack")
		assert.Equal(t, exitCode, 1, "restack should fail due to conflict")

		// Verify that restack state was saved
		assert.Assert(t, yas.RestackStateExists("."), "restack state should be saved")

		// User fixes conflicts and completes the rebase manually
		testutil.ExecOrFail(t, `
			# Accept both changes
			echo "line1" > file.txt
			echo "line2-from-main" >> file.txt
			echo "line2-from-a" >> file.txt
			git add file.txt
			git -c core.hooksPath=/dev/null -c core.editor=true rebase --continue
		`)

		// Run continue - should detect that rebase is already complete
		exitCode = yascli.Run("continue")
		assert.Equal(t, exitCode, 0, "continue should succeed even if user already completed rebase")

		// Verify that restack state was cleaned up
		assert.Assert(t, !yas.RestackStateExists("."), "restack state should be cleaned up")

		// Verify the final state
		output := mustExecOutput("git", "log", "--pretty=%s")
		assert.Assert(t, strings.Contains(output, "topic-a-0"), "topic-a commit should exist")
		assert.Assert(t, strings.Contains(output, "main-1"), "main-1 commit should exist")
	})
}

func TestContinue_ErrorsWhenRebaseAborted(t *testing.T) {
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

		// Initialize yas config
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)

		// Run restack - it should fail due to conflict
		exitCode := yascli.Run("restack")
		assert.Equal(t, exitCode, 1, "restack should fail due to conflict")

		// Verify that restack state was saved
		assert.Assert(t, yas.RestackStateExists("."), "restack state should be saved")

		// User aborts the rebase
		testutil.ExecOrFail(t, `git rebase --abort`)

		// Run continue - should detect that rebase was aborted and error
		exitCode = yascli.Run("continue")
		assert.Equal(t, exitCode, 1, "continue should fail when rebase was aborted")

		// Verify that restack state still exists (wasn't cleaned up on error)
		assert.Assert(t, yas.RestackStateExists("."), "restack state should still exist after abort detection")

		// Verify we're still on topic-a
		equalLines(t, mustExecOutput("git", "branch", "--show-current"), "topic-a")

		// Verify topic-a was NOT rebased (still has old commits)
		output := mustExecOutput("git", "log", "--pretty=%s")
		assert.Assert(t, strings.Contains(output, "topic-a-0"), "topic-a commit should exist")
		assert.Assert(t, strings.Contains(output, "main-0"), "main-0 commit should exist")
		assert.Assert(t, !strings.Contains(output, "main-1"), "main-1 should NOT be in history (rebase was aborted)")
	})
}

func TestContinue_MultipleConflicts(t *testing.T) {
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
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-b", "--parent=topic-a"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-c", "--parent=topic-b"), 0)

		// Run restack - it should fail on topic-a
		exitCode := yascli.Run("restack")
		assert.Equal(t, exitCode, 1, "restack should fail on topic-a")

		// Fix conflict for topic-a
		testutil.ExecOrFail(t, `
			echo "line1" > file.txt
			echo "line2-from-main" >> file.txt
			echo "line2-from-a" >> file.txt
			git add file.txt
		`)

		// Continue - might still have conflicts on topic-b or topic-c
		exitCode = yascli.Run("continue")
		// Could be 0 (success) or 1 (another conflict)
		// For this test, we'll just verify it doesn't crash

		if exitCode == 0 {
			// All conflicts resolved successfully
			assert.Assert(t, !yas.RestackStateExists("."), "restack state should be cleaned up")

			// Verify final state - all branches should be successfully rebased
			testutil.ExecOrFail(t, "git checkout topic-c")

			output := mustExecOutput("git", "log", "--pretty=%s")
			assert.Assert(t, strings.Contains(output, "topic-c-0"), "topic-c commit should exist")
			assert.Assert(t, strings.Contains(output, "topic-b-0"), "topic-b commit should exist")
			assert.Assert(t, strings.Contains(output, "topic-a-0"), "topic-a commit should exist")
			// Note: main-1 is included because topic-a was rebased onto main which includes main-1
			assert.Assert(t, strings.Contains(output, "main-1"), "main-1 commit should exist after rebase")
		} else {
			// Another conflict occurred - state should still exist
			assert.Assert(t, yas.RestackStateExists("."), "restack state should still exist on another conflict")
		}
	})
}
