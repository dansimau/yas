package test

import (
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"github.com/dansimau/yas/pkg/yascli"
	"gotest.tools/v3/assert"
)

// TestMove_BasicMove tests moving a branch to a new parent.
func TestMove_BasicMove(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main

			# main
			echo "line1" > file.txt
			git add file.txt
			git commit -m "main-0"

			# feature-a
			git checkout -b feature-a
			echo "a" > a.txt
			git add a.txt
			git commit -m "feature-a-0"

			# topic-b: branch from main
			git checkout main
			git checkout -b topic-b
			echo "b" > b.txt
			git add b.txt
			git commit -m "topic-b-0"

			# feature-a-child: branch from feature-a
			git checkout feature-a
			git checkout -b feature-a-child
			echo "child" > child.txt
			git add child.txt
			git commit -m "feature-a-child-0"

			# Back to feature-a to run move
			git checkout feature-a
		`)

		// Initialize yas config
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "feature-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "topic-b", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "feature-a-child", "--parent=feature-a"), 0)

		// Move feature-a (and its child) to topic-b
		exitCode := yascli.Run("move", "--onto=topic-b")
		assert.Equal(t, exitCode, 0, "move should succeed")

		// Verify feature-a is now based on topic-b
		logOutput := mustExecOutput("git", "log", "--pretty=%s")
		assert.Assert(t, strings.Contains(logOutput, "topic-b-0"), "feature-a should include topic-b commit")

		// Verify feature-a-child is still a child and also includes topic-b
		testutil.ExecOrFail(t, "git checkout feature-a-child")

		logOutput = mustExecOutput("git", "log", "--pretty=%s")
		assert.Assert(t, strings.Contains(logOutput, "topic-b-0"), "feature-a-child should include topic-b commit")
		assert.Assert(t, strings.Contains(logOutput, "feature-a-0"), "feature-a-child should include feature-a commit")

		// Verify parent metadata was updated
		yasInstance, err := yas.NewFromRepository(".")
		assert.NilError(t, err)

		branches := yasInstance.TrackedBranches()

		var featureAMetadata yas.BranchMetadata

		for _, b := range branches {
			if b.Name == "feature-a" {
				featureAMetadata = b

				break
			}
		}

		assert.Equal(t, featureAMetadata.Parent, "topic-b", "feature-a parent should be updated to topic-b")
	})
}

// TestMove_WithConflicts tests that move handles conflicts correctly with state save/resume.
func TestMove_WithConflicts(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main

			# main
			echo "line1" > file.txt
			git add file.txt
			git commit -m "main-0"

			# feature-a: modify file
			git checkout -b feature-a
			echo "line2-from-a" >> file.txt
			git add file.txt
			git commit -m "feature-a-0"

			# topic-b: modify the same file differently
			git checkout main
			git checkout -b topic-b
			echo "line2-from-b" >> file.txt
			git add file.txt
			git commit -m "topic-b-0"

			# Back to feature-a to run move
			git checkout feature-a
		`)

		// Initialize yas config
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "feature-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "topic-b", "--parent=main"), 0)

		// Try to move feature-a to topic-b - should fail with conflicts
		exitCode := yascli.Run("move", "--onto=topic-b")
		assert.Equal(t, exitCode, 1, "move should fail due to conflicts")

		// Verify restack state was saved
		assert.Assert(t, yas.RestackStateExists("."), "restack state should be saved")

		// Verify we're in the middle of a rebase
		testutil.ExecOrFail(t, "test -d .git/rebase-merge || test -d .git/rebase-apply")

		// Resolve the conflict
		testutil.ExecOrFail(t, `
			echo "line1" > file.txt
			echo "line2-from-b" >> file.txt
			echo "line2-from-a" >> file.txt
			git add file.txt
		`)

		// Continue the move
		exitCode = yascli.Run("continue")
		assert.Equal(t, exitCode, 0, "continue should succeed")

		// Verify restack state was cleaned up
		assert.Assert(t, !yas.RestackStateExists("."), "restack state should be cleaned up")

		// Verify feature-a is now based on topic-b
		logOutput := mustExecOutput("git", "log", "--pretty=%s")
		assert.Assert(t, strings.Contains(logOutput, "topic-b-0"), "feature-a should include topic-b commit")
	})
}

// TestMove_Abort tests that aborting a move works correctly.
func TestMove_Abort(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main

			# main
			echo "line1" > file.txt
			git add file.txt
			git commit -m "main-0"

			# feature-a: modify file
			git checkout -b feature-a
			echo "line2-from-a" >> file.txt
			git add file.txt
			git commit -m "feature-a-0"

			# topic-b: modify the same file differently
			git checkout main
			git checkout -b topic-b
			echo "line2-from-b" >> file.txt
			git add file.txt
			git commit -m "topic-b-0"

			# Back to feature-a to run move
			git checkout feature-a
		`)

		// Capture the original commit of feature-a
		originalCommit := mustExecOutput("git", "rev-parse", "HEAD")
		originalCommit = strings.TrimSpace(originalCommit)

		// Initialize yas config
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "feature-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "topic-b", "--parent=main"), 0)

		// Try to move feature-a to topic-b - should fail with conflicts
		exitCode := yascli.Run("move", "--onto=topic-b")
		assert.Equal(t, exitCode, 1, "move should fail due to conflicts")

		// Verify restack state was saved
		assert.Assert(t, yas.RestackStateExists("."), "restack state should be saved")

		// Abort the move
		exitCode = yascli.Run("abort")
		assert.Equal(t, exitCode, 0, "abort should succeed")

		// Verify restack state was cleaned up
		assert.Assert(t, !yas.RestackStateExists("."), "restack state should be cleaned up")

		// Verify we're on feature-a
		equalLines(t, mustExecOutput("git", "branch", "--show-current"), "feature-a")

		// Verify feature-a was reset by git rebase --abort
		currentCommit := strings.TrimSpace(mustExecOutput("git", "rev-parse", "HEAD"))
		assert.Equal(t, currentCommit, originalCommit, "branch should be back at original commit after rebase abort")

		// Verify feature-a still has old commits (not moved)
		logOutput := mustExecOutput("git", "log", "--pretty=%s")
		assert.Assert(t, strings.Contains(logOutput, "feature-a-0"), "feature-a commit should exist")
		assert.Assert(t, strings.Contains(logOutput, "main-0"), "main-0 commit should exist")
		assert.Assert(t, !strings.Contains(logOutput, "topic-b-0"), "topic-b-0 should NOT be in history")
	})
}

// TestMove_FatalError tests that fatal errors don't save state.
func TestMove_FatalError(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main

			# main
			echo "line1" > file.txt
			git add file.txt
			git commit -m "main-0"

			# feature-a
			git checkout -b feature-a
			echo "line2" >> file.txt
			git add file.txt
			git commit -m "feature-a-0"

			# topic-b
			git checkout main
			git checkout -b topic-b
			echo "line3" > topic.txt
			git add topic.txt
			git commit -m "topic-b-0"

			# Back to feature-a
			git checkout feature-a

			# Create unstashed changes that will cause rebase to fail
			echo "uncommitted change" >> file.txt
		`)

		// Initialize yas config
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "feature-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "topic-b", "--parent=main"), 0)

		// Try to move - should fail due to unstashed changes
		exitCode := yascli.Run("move", "--onto=topic-b")
		assert.Equal(t, exitCode, 1, "move should fail due to unstashed changes")

		// Verify that restack state was NOT saved (fatal error, not conflict)
		assert.Assert(t, !yas.RestackStateExists("."), "restack state should NOT be saved for fatal errors")

		// Verify we're still on feature-a
		equalLines(t, mustExecOutput("git", "branch", "--show-current"), "feature-a")

		// Verify no rebase is in progress
		testutil.ExecOrFail(t, "test ! -d .git/rebase-merge")
		testutil.ExecOrFail(t, "test ! -d .git/rebase-apply")
	})
}

// TestMove_WithMultipleDescendants tests moving a branch with multiple descendants.
func TestMove_WithMultipleDescendants(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main

			# main
			echo "line1" > file.txt
			git add file.txt
			git commit -m "main-0"

			# feature-a
			git checkout -b feature-a
			echo "a" > a.txt
			git add a.txt
			git commit -m "feature-a-0"

			# feature-a-child-1
			git checkout -b feature-a-child-1
			echo "child1" > child1.txt
			git add child1.txt
			git commit -m "child1-0"

			# feature-a-child-2: branch from feature-a
			git checkout feature-a
			git checkout -b feature-a-child-2
			echo "child2" > child2.txt
			git add child2.txt
			git commit -m "child2-0"

			# topic-b: new parent for feature-a
			git checkout main
			git checkout -b topic-b
			echo "b" > b.txt
			git add b.txt
			git commit -m "topic-b-0"

			# Back to feature-a to run move
			git checkout feature-a
		`)

		// Initialize yas config
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "feature-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "feature-a-child-1", "--parent=feature-a"), 0)
		assert.Equal(t, yascli.Run("add", "feature-a-child-2", "--parent=feature-a"), 0)
		assert.Equal(t, yascli.Run("add", "topic-b", "--parent=main"), 0)

		// Move feature-a (and both children) to topic-b
		exitCode := yascli.Run("move", "--onto=topic-b")
		assert.Equal(t, exitCode, 0, "move should succeed")

		// Verify all branches include topic-b-0
		for _, branch := range []string{"feature-a", "feature-a-child-1", "feature-a-child-2"} {
			testutil.ExecOrFail(t, "git checkout "+branch)

			logOutput := mustExecOutput("git", "log", "--pretty=%s")
			assert.Assert(t, strings.Contains(logOutput, "topic-b-0"),
				"%s should include topic-b commit", branch)
		}

		// Verify parent metadata was updated
		yasInstance, err := yas.NewFromRepository(".")
		assert.NilError(t, err)

		branches := yasInstance.TrackedBranches()

		var featureAMetadata yas.BranchMetadata

		for _, b := range branches {
			if b.Name == "feature-a" {
				featureAMetadata = b

				break
			}
		}

		assert.Equal(t, featureAMetadata.Parent, "topic-b", "feature-a parent should be updated to topic-b")
	})
}

// TestMove_CannotMoveTrunk tests that moving trunk branch is not allowed.
func TestMove_CannotMoveTrunk(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch file.txt
			git add file.txt
			git commit -m "main-0"
		`)

		// Initialize yas config
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)

		// Try to move trunk - should fail
		exitCode := yascli.Run("move", "--onto=main")
		assert.Equal(t, exitCode, 1, "move should fail for trunk branch")
	})
}
