package test

import (
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

// TestMove_BasicMove tests moving a branch to a new parent.
func TestMove_BasicMove(t *testing.T) {
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
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a-child", "--parent=feature-a").Err())

	// Move feature-a (and its child) to topic-b
	result := cli.Run("move", "--onto=topic-b")
	assert.NilError(t, result.Err(), "move should succeed")

	// Verify feature-a is now based on topic-b
	logOutput := mustExecOutput(tempDir, "git", "log", "--pretty=%s")
	assert.Assert(t, strings.Contains(logOutput, "topic-b-0"), "feature-a should include topic-b commit")

	// Verify feature-a-child is still a child and also includes topic-b
	testutil.ExecOrFail(t, tempDir, "git checkout feature-a-child")

	logOutput = mustExecOutput(tempDir, "git", "log", "--pretty=%s")
	assert.Assert(t, strings.Contains(logOutput, "topic-b-0"), "feature-a-child should include topic-b commit")
	assert.Assert(t, strings.Contains(logOutput, "feature-a-0"), "feature-a-child should include feature-a commit")

	// Verify parent metadata was updated
	yasInstance, err := yas.NewFromRepository(tempDir)
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
}

// TestMove_WithConflicts tests that move handles conflicts correctly with state save/resume.
func TestMove_WithConflicts(t *testing.T) {
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
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=main").Err())

	// Try to move feature-a to topic-b - should fail with conflicts
	result := cli.Run("move", "--onto=topic-b")
	assert.Equal(t, result.ExitCode(), 1, "move should fail due to conflicts")

	// Verify restack state was saved
	assert.Assert(t, assertRestackStateExists(t, tempDir), "restack state should be saved")

	// Verify we're in the middle of a rebase
	testutil.ExecOrFail(t, tempDir, "test -d .git/rebase-merge || test -d .git/rebase-apply")

	// Resolve the conflict
	testutil.ExecOrFail(t, tempDir, `
		echo "line1" > file.txt
		echo "line2-from-b" >> file.txt
		echo "line2-from-a" >> file.txt
		git add file.txt
	`)

	// Continue the move
	assert.NilError(t, cli.Run("continue").Err(), "continue should succeed")

	// Verify restack state was cleaned up
	assert.Assert(t, !assertRestackStateExists(t, tempDir), "restack state should be cleaned up")

	// Verify feature-a is now based on topic-b
	logOutput := mustExecOutput(tempDir, "git", "log", "--pretty=%s")
	assert.Assert(t, strings.Contains(logOutput, "topic-b-0"), "feature-a should include topic-b commit")
}

// TestMove_Abort tests that aborting a move works correctly.
func TestMove_Abort(t *testing.T) {
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
	originalCommit := mustExecOutput(tempDir, "git", "rev-parse", "HEAD")
	originalCommit = strings.TrimSpace(originalCommit)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=main").Err())

	// Try to move feature-a to topic-b - should fail with conflicts
	result := cli.Run("move", "--onto=topic-b")
	assert.Equal(t, result.ExitCode(), 1, "move should fail due to conflicts")

	// Verify restack state was saved
	assert.Assert(t, assertRestackStateExists(t, tempDir), "restack state should be saved")

	// Abort the move
	assert.NilError(t, cli.Run("abort").Err(), "abort should succeed")

	// Verify restack state was cleaned up
	assert.Assert(t, !assertRestackStateExists(t, tempDir), "restack state should be cleaned up")

	// Verify we're on feature-a
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--show-current"), "feature-a")

	// Verify feature-a was reset by git rebase --abort
	currentCommit := strings.TrimSpace(mustExecOutput(tempDir, "git", "rev-parse", "HEAD"))
	assert.Equal(t, currentCommit, originalCommit, "branch should be back at original commit after rebase abort")

	// Verify feature-a still has old commits (not moved)
	logOutput := mustExecOutput(tempDir, "git", "log", "--pretty=%s")
	assert.Assert(t, strings.Contains(logOutput, "feature-a-0"), "feature-a commit should exist")
	assert.Assert(t, strings.Contains(logOutput, "main-0"), "main-0 commit should exist")
	assert.Assert(t, !strings.Contains(logOutput, "topic-b-0"), "topic-b-0 should NOT be in history")
}

// TestMove_FatalError tests that fatal errors don't save state.
func TestMove_FatalError(t *testing.T) {
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
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=main").Err())

	// Try to move - should fail due to unstashed changes
	result := cli.Run("move", "--onto=topic-b")
	assert.Equal(t, result.ExitCode(), 1, "move should fail due to unstashed changes")

	// Verify that restack state was NOT saved (fatal error, not conflict)
	assert.Assert(t, !assertRestackStateExists(t, tempDir), "restack state should NOT be saved for fatal errors")

	// Verify we're still on feature-a
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--show-current"), "feature-a")

	// Verify no rebase is in progress
	testutil.ExecOrFail(t, tempDir, "test ! -d .git/rebase-merge")
	testutil.ExecOrFail(t, tempDir, "test ! -d .git/rebase-apply")
}

// TestMove_WithMultipleDescendants tests moving a branch with multiple descendants.
func TestMove_WithMultipleDescendants(t *testing.T) {
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
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a-child-1", "--parent=feature-a").Err())
	assert.NilError(t, cli.Run("add", "feature-a-child-2", "--parent=feature-a").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=main").Err())

	// Move feature-a (and both children) to topic-b
	assert.NilError(t, cli.Run("move", "--onto=topic-b").Err(), "move should succeed")

	// Verify all branches include topic-b-0
	for _, branch := range []string{"feature-a", "feature-a-child-1", "feature-a-child-2"} {
		testutil.ExecOrFail(t, tempDir, "git checkout "+branch)

		logOutput := mustExecOutput(tempDir, "git", "log", "--pretty=%s")
		assert.Assert(t, strings.Contains(logOutput, "topic-b-0"),
			"%s should include topic-b commit", branch)
	}

	// Verify parent metadata was updated
	yasInstance, err := yas.NewFromRepository(tempDir)
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
}

// TestMove_CannotMoveTrunk tests that moving trunk branch is not allowed.
func TestMove_CannotMoveTrunk(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch file.txt
		git add file.txt
		git commit -m "main-0"
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())

	// Try to move trunk - should fail
	result := cli.Run("move", "--onto=main")
	assert.Equal(t, result.ExitCode(), 1, "move should fail for trunk branch")
}
