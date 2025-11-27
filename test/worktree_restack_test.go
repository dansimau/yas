package test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

// TestWorktree_ContinueResumeAfterConflict tests that yas continue works correctly
// when the branch being rebased has a linked worktree. The rebase state resides
// in the worktree, not the primary repo.
func TestWorktree_ContinueResumeAfterConflict(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	worktreePath := filepath.Join(tempDir, "worktrees", "topic-a")

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

		# Go back to main and create worktree for topic-a
		git checkout main
		git worktree add `+worktreePath+` topic-a

		# update main: modify the same file differently (will cause conflict)
		echo "line2-from-main" >> file.txt
		git add file.txt
		git commit -m "main-1"
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Run restack from primary repo - it should fail due to conflict
	// The rebase will happen in the worktree
	result := cli.Run("restack")
	assert.Equal(t, result.ExitCode(), 1, "restack should fail due to conflict")

	// Verify that restack state was saved
	assert.Assert(t, assertRestackStateExists(t, tempDir), "restack state should be saved")

	// Load and verify the state
	state, err := yas.LoadRestackState(tempDir)
	assert.NilError(t, err)
	assert.Equal(t, state.CurrentBranch, "topic-a", "current branch should be topic-a")
	assert.Equal(t, state.CurrentParent, "main", "current parent should be main")

	// Verify rebase is in progress in the WORKTREE (not primary repo)
	// In a linked worktree, the rebase state is in the git dir, not .git/
	testutil.ExecOrFail(t, worktreePath, "test -d $(git rev-parse --git-dir)/rebase-merge || test -d $(git rev-parse --git-dir)/rebase-apply")

	// Fix the conflict in the worktree
	testutil.ExecOrFail(t, worktreePath, `
		# Accept both changes
		echo "line1" > file.txt
		echo "line2-from-main" >> file.txt
		echo "line2-from-a" >> file.txt
		git add file.txt
	`)

	// Run continue from primary repo - should detect and resume rebase in worktree
	assert.NilError(t, cli.Run("continue").Err(), "continue should succeed")

	// Verify that restack state was cleaned up
	assert.Assert(t, !assertRestackStateExists(t, tempDir), "restack state should be cleaned up")

	// Verify we're back on main in primary repo
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--show-current"), "main")

	// Verify the final state - topic-a should be rebased onto main
	output := mustExecOutput(worktreePath, "git", "log", "--pretty=%s")
	assert.Assert(t, strings.Contains(output, "topic-a-0"), "topic-a commit should exist")
	assert.Assert(t, strings.Contains(output, "main-1"), "main-1 commit should exist after rebase")
}

// TestWorktree_AbortAbortsRebaseInWorktree tests that yas abort works correctly
// when the branch being rebased has a linked worktree.
func TestWorktree_AbortAbortsRebaseInWorktree(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	worktreePath := filepath.Join(tempDir, "worktrees", "topic-a")

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

		# Go back to main and create worktree for topic-a
		git checkout main
		git worktree add `+worktreePath+` topic-a

		# update main: modify the same file differently (will cause conflict)
		echo "line2-from-main" >> file.txt
		git add file.txt
		git commit -m "main-1"
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Run restack - it should fail due to conflict
	result := cli.Run("restack")
	assert.Equal(t, result.ExitCode(), 1, "restack should fail due to conflict")

	// Verify rebase is in progress in the worktree
	// In a linked worktree, the rebase state is in the git dir, not .git/
	testutil.ExecOrFail(t, worktreePath, "test -d $(git rev-parse --git-dir)/rebase-merge || test -d $(git rev-parse --git-dir)/rebase-apply")

	// Run abort from primary repo - should abort rebase in worktree
	assert.NilError(t, cli.Run("abort").Err(), "abort should succeed")

	// Verify that restack state was cleaned up
	assert.Assert(t, !assertRestackStateExists(t, tempDir), "restack state should be cleaned up")

	// Verify no rebase is in progress in the worktree
	testutil.ExecOrFail(t, worktreePath, "test ! -d $(git rev-parse --git-dir)/rebase-merge")
	testutil.ExecOrFail(t, worktreePath, "test ! -d $(git rev-parse --git-dir)/rebase-apply")

	// Verify topic-a was NOT rebased (still has old commits)
	output := mustExecOutput(worktreePath, "git", "log", "--pretty=%s")
	assert.Assert(t, strings.Contains(output, "topic-a-0"), "topic-a commit should exist")
	assert.Assert(t, strings.Contains(output, "main-0"), "main-0 commit should exist")
	assert.Assert(t, !strings.Contains(output, "main-1"), "main-1 should NOT be in history (rebase was aborted)")
}

// TestWorktree_RestackWithMultipleBranchesOneInWorktree tests restacking a stack
// where one branch in the middle has a worktree.
func TestWorktree_RestackWithMultipleBranchesOneInWorktree(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	worktreePath := filepath.Join(tempDir, "worktrees", "topic-a")

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		touch main
		git add main
		git commit -m "main-0"

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# topic-b (child of topic-a)
		git checkout -b topic-b
		touch b
		git add b
		git commit -m "topic-b-0"

		# Go back to main
		git checkout main

		# Create worktree for topic-a
		git worktree add `+worktreePath+` topic-a

		# Update main
		touch main-1
		git add main-1
		git commit -m "main-1"
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())

	// Run restack - should successfully rebase both branches
	// topic-a will be rebased in its worktree, topic-b in primary repo
	assert.NilError(t, cli.Run("restack").Err(), "restack should succeed")

	// Verify topic-a was rebased (check in worktree)
	output := mustExecOutput(worktreePath, "git", "log", "--pretty=%s")
	assert.Assert(t, strings.Contains(output, "topic-a-0"), "topic-a commit should exist")
	assert.Assert(t, strings.Contains(output, "main-1"), "main-1 commit should exist after rebase")

	// Verify topic-b was rebased (check in primary repo)
	testutil.ExecOrFail(t, tempDir, "git checkout topic-b")
	output = mustExecOutput(tempDir, "git", "log", "--pretty=%s")
	assert.Assert(t, strings.Contains(output, "topic-b-0"), "topic-b commit should exist")
	assert.Assert(t, strings.Contains(output, "topic-a-0"), "topic-a commit should exist")
	assert.Assert(t, strings.Contains(output, "main-1"), "main-1 commit should exist after rebase")
}
