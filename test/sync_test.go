package test

import (
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

func TestSync_RestacksChildrenOntoParentWhenMergedPRDeleted(t *testing.T) {
	_, cleanup := setupMockCommandsWithPR(t, mockPROptions{
		ID:    "PR_kwDOTest123",
		State: "MERGED",
		URL:   "https://github.com/test/test/pull/42",
	})
	defer cleanup()

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git remote add origin https://github.com/test/test.git

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
		`)

		// Initialize yas config
		cfg := yas.Config{
			RepoDirectory: ".",
			TrunkBranch:   "main",
		}
		_, err := yas.WriteConfig(cfg)
		assert.NilError(t, err)

		// Create YAS instance and track branches
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)
		err = y.SetParent("topic-a", "main", "")
		assert.NilError(t, err)
		err = y.SetParent("topic-b", "topic-a", "")
		assert.NilError(t, err)
		err = y.SetParent("topic-c", "topic-b", "")
		assert.NilError(t, err)

		// Simulate that topic-b has a merged PR by creating PR metadata
		// This will be fetched during sync
		err = y.RefreshRemoteStatus("topic-b")
		assert.NilError(t, err)

		// Verify initial state: topic-b is a child of topic-a
		branchMetadata := y.TrackedBranches()
		topicB, exists := branchMetadata.Get("topic-b")
		assert.Assert(t, exists, "topic-b should exist")
		assert.Equal(t, topicB.Parent, "topic-a")

		topicC, exists := branchMetadata.Get("topic-c")
		assert.Assert(t, exists, "topic-c should exist")
		assert.Equal(t, topicC.Parent, "topic-b")

		// Call DeleteMergedBranch on topic-b
		err = y.DeleteMergedBranch("topic-b")
		assert.NilError(t, err)

		// Reload the instance to get fresh data
		y, err = yas.NewFromRepository(".")
		assert.NilError(t, err)

		// Verify topic-b is deleted
		branchMetadata = y.TrackedBranches()
		_, exists = branchMetadata.Get("topic-b")
		assert.Assert(t, !exists, "topic-b should be deleted")

		// Verify topic-c's parent is now topic-a (was topic-b)
		topicC, exists = branchMetadata.Get("topic-c")
		assert.Assert(t, exists, "topic-c should still exist")
		assert.Equal(t, topicC.Parent, "topic-a", "topic-c should now be a child of topic-a")

		// Verify git history: topic-c should be rebased onto topic-a
		// and topic-b's commits should be removed
		testutil.ExecOrFail(t, "git checkout topic-c")
		equalLines(t, mustExecOutput("git", "log", "--pretty=%D : %s"), `
			HEAD -> topic-c : topic-c-0
			topic-a : topic-a-0
			main : main-0
		`)
	})
}

func TestSync_HandlesMultipleChildrenWhenParentMerged(t *testing.T) {
	_, cleanup := setupMockCommandsWithPR(t, mockPROptions{
		ID:    "PR_kwDOTest123",
		State: "MERGED",
		URL:   "https://github.com/test/test/pull/42",
	})
	defer cleanup()

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git remote add origin https://github.com/test/test.git

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
		cfg := yas.Config{
			RepoDirectory: ".",
			TrunkBranch:   "main",
		}
		_, err := yas.WriteConfig(cfg)
		assert.NilError(t, err)

		// Create YAS instance and track branches
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)
		err = y.SetParent("topic-a", "main", "")
		assert.NilError(t, err)
		err = y.SetParent("topic-b", "topic-a", "")
		assert.NilError(t, err)
		err = y.SetParent("topic-c", "topic-a", "")
		assert.NilError(t, err)

		// Simulate that topic-a has a merged PR
		err = y.RefreshRemoteStatus("topic-a")
		assert.NilError(t, err)

		// Call DeleteMergedBranch on topic-a
		err = y.DeleteMergedBranch("topic-a")
		assert.NilError(t, err)

		// Reload the instance to get fresh data
		y, err = yas.NewFromRepository(".")
		assert.NilError(t, err)

		// Verify topic-a is deleted
		branchMetadata := y.TrackedBranches()
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
		testutil.ExecOrFail(t, "git checkout topic-b")
		equalLines(t, mustExecOutput("git", "log", "--pretty=%D : %s"), `
			HEAD -> topic-b : topic-b-0
			main : main-0
		`)

		testutil.ExecOrFail(t, "git checkout topic-c")
		equalLines(t, mustExecOutput("git", "log", "--pretty=%D : %s"), `
			HEAD -> topic-c : topic-c-0
			main : main-0
		`)
	})
}

func TestSync_ErrorsWhenMergedBranchHasNoParent(t *testing.T) {
	_, cleanup := setupMockCommandsWithPR(t, mockPROptions{
		ID:    "PR_kwDOTest123",
		State: "MERGED",
		URL:   "https://github.com/test/test/pull/42",
	})
	defer cleanup()

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git remote add origin https://github.com/test/test.git

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

		// Initialize yas config
		cfg := yas.Config{
			RepoDirectory: ".",
			TrunkBranch:   "main",
		}
		_, err := yas.WriteConfig(cfg)
		assert.NilError(t, err)

		// Create YAS instance but DON'T set parent for topic-a
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)

		// Manually add topic-a to tracked branches without a parent
		branchMetadata := y.TrackedBranches()
		topicA, _ := branchMetadata.Get("topic-a")
		topicA.Name = "topic-a"
		topicA.Parent = "" // No parent set

		// Simulate merged PR metadata
		err = y.RefreshRemoteStatus("topic-a")
		assert.NilError(t, err)

		// Try to delete merged branch without parent - should error
		err = y.DeleteMergedBranch("topic-a")
		assert.ErrorContains(t, err, "has no parent branch set")
		assert.ErrorContains(t, err, "cannot safely delete merged branch")
	})
}
