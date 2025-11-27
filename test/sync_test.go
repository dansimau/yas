package test

import (
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/stringutil"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

func TestSync_RestacksChildrenOntoParentWhenMergedPRDeleted(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Mock gh pr list to return merged PR for topic-b
	mockGitHubPRForBranch(cli, "topic-b", yas.PullRequestMetadata{
		ID:          "PR_kwDOTest123",
		State:       "MERGED",
		URL:         "https://github.com/test/test/pull/42",
		IsDraft:     false,
		BaseRefName: "topic-a",
	})

	mockGitHubPRForBranch(cli, "main", yas.PullRequestMetadata{})

	// Git pull should be successful
	cli.Mock("git", "pull", gocmdtester.AnyFurtherArgs).WithStdout("Already up to date.\n")

	// Pass through all other git commands
	cli.Mock("git", gocmdtester.AnyFurtherArgs).WithPassthroughExec()

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		# Set up "remote" bare repository
		git init --bare {{.fakeOrigin}}

		# Initialize local repo with main branch
		git init --initial-branch=main

		git remote add origin {{.fakeOrigin}}
		git branch --set-upstream-to=origin/main main
		git push -u origin main

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

		# topic-c (child of topic-b)
		git checkout -b topic-c
		touch c
		git add c
		git commit -m "topic-c-0"

		# back to main
		git checkout main
	`, map[string]string{
		"fakeOrigin": fakeOrigin,
	}))

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())
	assert.NilError(t, cli.Run("add", "topic-c", "--parent=topic-b").Err())

	// Refresh topic-b to populate PR metadata
	assert.NilError(t, cli.Run("refresh", "topic-b").Err())

	// Verify initial state
	yasInstance, err := yas.NewFromRepository(tempDir)
	assert.NilError(t, err)

	branchMetadata := yasInstance.TrackedBranches()
	topicB, exists := branchMetadata.Get("topic-b")
	assert.Assert(t, exists, "topic-b should exist")
	assert.Equal(t, topicB.Parent, "topic-a")

	topicC, exists := branchMetadata.Get("topic-c")
	assert.Assert(t, exists, "topic-c should exist")
	assert.Equal(t, topicC.Parent, "topic-b")

	// Delete merged branch topic-b
	err = yasInstance.DeleteBranch("topic-b")
	assert.NilError(t, err)

	// Sync to trigger reparenting of children
	assert.NilError(t, cli.Run("sync", "--restack").Err())

	// Reload state file
	assert.NilError(t, yasInstance.Reload())

	// Verify topic-b is deleted
	branchMetadata = yasInstance.TrackedBranches()
	_, exists = branchMetadata.Get("topic-b")
	assert.Assert(t, !exists, "topic-b should be deleted")

	// Verify topic-c's parent is now topic-a (was topic-b)
	topicC, exists = branchMetadata.Get("topic-c")
	assert.Assert(t, exists, "topic-c should still exist")
	assert.Equal(t, topicC.Parent, "topic-a", "topic-c should now be a child of topic-a")

	// Verify git history: topic-c should be rebased onto topic-a
	// and topic-b's commits should be removed
	testutil.ExecOrFail(t, tempDir, "git checkout topic-c")
	equalLines(t, mustExecOutput(tempDir, "git", "log", "--pretty=%D : %s"), `
		HEAD -> topic-c : topic-c-0
		topic-a : topic-a-0
		main : main-0
	`)
}

func TestSync_HandlesMultipleChildrenWhenParentMerged(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Mock gh pr list to return merged PR for topic-a
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		ID:          "PR_kwDOTest123",
		State:       "MERGED",
		URL:         "https://github.com/test/test/pull/42",
		IsDraft:     false,
		BaseRefName: "main",
	})

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		git remote add origin https://fake.origin/test/test.git

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

		# topic-c (also child of topic-a, forked from same point)
		git checkout topic-a
		git checkout -b topic-c
		touch c
		git add c
		git commit -m "topic-c-0"

		# back to main
		git checkout main
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())
	assert.NilError(t, cli.Run("add", "topic-c", "--parent=topic-a").Err())

	// Refresh topic-a to populate PR metadata
	assert.NilError(t, cli.Run("refresh", "topic-a").Err())

	// Create YAS instance and delete merged branch
	yasInstance, err := yas.NewFromRepository(tempDir)
	assert.NilError(t, err)

	err = yasInstance.DeleteBranch("topic-a")
	assert.NilError(t, err)

	// Restack from trunk to trigger reparenting of all children
	assert.NilError(t, cli.Run("restack", "main").Err())

	// Reload to get fresh data
	assert.NilError(t, yasInstance.Reload())

	// Verify topic-a is deleted
	branchMetadata := yasInstance.TrackedBranches()
	_, exists := branchMetadata.Get("topic-a")
	assert.Assert(t, !exists, "topic-a should be deleted")

	// Verify both children now point to main
	topicB, exists := branchMetadata.Get("topic-b")
	assert.Assert(t, exists, "topic-b should still exist")
	assert.Equal(t, topicB.Parent, "main", "topic-b should now be a child of main")

	topicC, exists := branchMetadata.Get("topic-c")
	assert.Assert(t, exists, "topic-c should still exist")
	assert.Equal(t, topicC.Parent, "main", "topic-c should now be a child of main")

	// Verify git history: topic-a's commits should be removed from both children
	testutil.ExecOrFail(t, tempDir, "git checkout topic-b")
	equalLines(t, mustExecOutput(tempDir, "git", "log", "--pretty=%D : %s"), `
		HEAD -> topic-b : topic-b-0
		main : main-0
	`)

	testutil.ExecOrFail(t, tempDir, "git checkout topic-c")
	equalLines(t, mustExecOutput(tempDir, "git", "log", "--pretty=%D : %s"), `
		HEAD -> topic-c : topic-c-0
		main : main-0
	`)
}

func TestSync_DeleteBranchWithNoParent(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Mock gh pr list to return merged PR for topic-a
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		ID:          "PR_kwDOTest123",
		State:       "MERGED",
		URL:         "https://github.com/test/test/pull/42",
		IsDraft:     false,
		BaseRefName: "main",
	})

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		git remote add origin https://fake.origin/test/test.git

		# main
		touch main
		git add main
		git commit -m "main-0"

		# topic-a (but we won't set a parent)
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# back to main
		git checkout main
	`)

	// Initialize yas config but DON'T set parent for topic-a
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())

	// Create YAS instance
	yasInstance, err := yas.NewFromRepository(tempDir)
	assert.NilError(t, err)

	// Refresh to populate PR metadata (but no parent set)
	assert.NilError(t, cli.Run("refresh", "topic-a").Err())

	// Delete branch without parent - should succeed (soft delete)
	err = yasInstance.DeleteBranch("topic-a")
	assert.NilError(t, err)

	// Reload and verify topic-a is no longer in tracked branches
	assert.NilError(t, yasInstance.Reload())

	branchMetadata := yasInstance.TrackedBranches()
	_, exists := branchMetadata.Get("topic-a")
	assert.Assert(t, !exists, "topic-a should be deleted from tracked branches")
}
