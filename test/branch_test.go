package test

import (
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"github.com/dansimau/yas/pkg/yascli"
	"gotest.tools/v3/assert"
)

func TestBranch_GetBranchList_ForInteractiveSwitcher(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Create simple branch structure: main -> topic-a
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"

			git checkout -b topic-a
			touch a
			git add a
			git commit -m "topic-a-0"
		`)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)

		// Test GetBranchList directly (used by SwitchBranchInteractive)
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)

		items, err := y.GetBranchList(false, false)
		assert.NilError(t, err)

		// Verify we get the expected branches
		assert.Equal(t, len(items), 2, "Should have 2 branches")

		// Find main and topic-a in the items
		foundMain := false
		foundTopicA := false

		for _, item := range items {
			if strings.Contains(item.ID, "main") {
				foundMain = true
			}

			if strings.Contains(item.ID, "topic-a") {
				foundTopicA = true
			}
		}

		assert.Assert(t, foundMain, "Should include main branch")
		assert.Assert(t, foundTopicA, "Should include topic-a branch")
	})
}

func TestBranch_GetBranchList_MultiLevelStack(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Create stack: main -> topic-a -> topic-b -> topic-c
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"

			git checkout -b topic-a
			touch a
			git add a
			git commit -m "topic-a-0"

			git checkout -b topic-b
			touch b
			git add b
			git commit -m "topic-b-0"

			git checkout -b topic-c
			touch c
			git add c
			git commit -m "topic-c-0"
		`)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-b", "--parent=topic-a"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-c", "--parent=topic-b"), 0)

		// Test GetBranchList for multi-level stack
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)

		items, err := y.GetBranchList(false, false)
		assert.NilError(t, err)

		// Verify we get all branches in the stack
		assert.Equal(t, len(items), 4, "Should have 4 branches")

		// Verify tree structure is displayed with proper indentation
		foundTreeChars := false

		for _, item := range items {
			if strings.Contains(item.Line, "├──") || strings.Contains(item.Line, "└──") {
				foundTreeChars = true

				break
			}
		}

		assert.Assert(t, foundTreeChars, "Should display tree characters")
	})
}

func TestBranch_GetBranchList_ForkedBranches(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Create fork: main -> topic-a -> [topic-b, topic-c]
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"

			git checkout -b topic-a
			touch a
			git add a
			git commit -m "topic-a-0"

			git checkout -b topic-b
			touch b
			git add b
			git commit -m "topic-b-0"

			git checkout topic-a
			git checkout -b topic-c
			touch c
			git add c
			git commit -m "topic-c-0"
		`)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-b", "--parent=topic-a"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-c", "--parent=topic-a"), 0)

		// Test GetBranchList for forked branches
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)

		items, err := y.GetBranchList(false, false)
		assert.NilError(t, err)

		// Verify we get all branches including the fork
		assert.Equal(t, len(items), 4, "Should have 4 branches")

		// Verify both branches are displayed
		foundTopicB := false
		foundTopicC := false

		for _, item := range items {
			if strings.Contains(item.ID, "topic-b") {
				foundTopicB = true
			}

			if strings.Contains(item.ID, "topic-c") {
				foundTopicC = true
			}
		}

		assert.Assert(t, foundTopicB, "Should display topic-b branch")
		assert.Assert(t, foundTopicC, "Should display topic-c branch")
	})
}

func TestBranch_GetBranchList_CurrentBranchHighlight(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Create simple structure and stay on topic-a
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"

			git checkout -b topic-a
			touch a
			git add a
			git commit -m "topic-a-0"
		`)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)

		// Test GetBranchList shows current branch with *
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)

		items, err := y.GetBranchList(false, false)
		assert.NilError(t, err)

		// Verify current branch is highlighted with *
		foundCurrentBranch := false

		for _, item := range items {
			if strings.Contains(item.ID, "topic-a") && strings.Contains(item.Line, "*") {
				foundCurrentBranch = true

				break
			}
		}

		assert.Assert(t, foundCurrentBranch, "Should highlight current branch with *")
	})
}

func TestBranch_GetBranchList_EmptyBranchList(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Create repo with only main branch (no tracked branches)
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"
		`)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)

		// Test GetBranchList with no tracked branches
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)

		items, err := y.GetBranchList(false, false)
		assert.NilError(t, err)

		// Should only have main branch
		assert.Equal(t, len(items), 1, "Should have 1 branch (main)")
		assert.Equal(t, items[0].ID, "main", "Should have main branch")
	})
}

func TestBranch_GetBranchList_SingleBranch(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Create repo with only main branch
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"
		`)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)

		// Test GetBranchList with single branch
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)

		items, err := y.GetBranchList(false, false)
		assert.NilError(t, err)

		// Should have only main branch
		assert.Equal(t, len(items), 1, "Should have 1 branch")
		assert.Equal(t, items[0].ID, "main", "Should have main branch")
	})
}

func TestBranch_GetBranchList_BranchNameFormatting(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Create branch with prefix
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"

			git checkout -b user/feature-branch
			touch feature
			git add feature
			git commit -m "feature-0"
		`)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=user/feature-branch", "--parent=main"), 0)

		// Test GetBranchList shows formatted branch name
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)

		items, err := y.GetBranchList(false, false)
		assert.NilError(t, err)

		// Should display formatted branch name with greyed prefix
		foundFormattedBranch := false

		for _, item := range items {
			if strings.Contains(item.ID, "user/feature-branch") {
				foundFormattedBranch = true

				break
			}
		}

		assert.Assert(t, foundFormattedBranch, "Should display formatted branch name")
	})
}

func TestBranchSwitch_ExistingLocalBranch(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"

			git checkout -b topic-a
			touch a
			git add a
			git commit -m "topic-a-0"

			git checkout -b topic-b
			touch b
			git add b
			git commit -m "topic-b-0"

			git checkout main
		`)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-b", "--parent=topic-a"), 0)

		// Switch to existing local branch using yas branch command
		assert.Equal(t, yascli.Run("branch", "topic-a"), 0)

		// Verify we're on the correct branch
		currentBranch := strings.TrimSpace(mustExecOutput("git", "branch", "--show-current"))
		assert.Equal(t, currentBranch, "topic-a")

		// Switch to another existing branch
		assert.Equal(t, yascli.Run("branch", "topic-b"), 0)

		// Verify we're on the correct branch
		currentBranch = strings.TrimSpace(mustExecOutput("git", "branch", "--show-current"))
		assert.Equal(t, currentBranch, "topic-b")
	})
}

func TestBranchSwitch_RemoteBranchInitiatesRefresh(t *testing.T) {
	cmdLogFile, cleanup := setupMockCommandsWithPR(t, mockPROptions{
		ID:    "PR_kwDOTest123",
		State: "OPEN",
		URL:   "https://github.com/test/test/pull/42",
	})
	defer cleanup()

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git remote add origin https://fake.origin/test/test.git

			touch main
			git add main
			git commit -m "main-0"

			git checkout -b topic-a
			touch a
			git add a
			git commit -m "topic-a-0"

			# Push topic-a to simulate it being a remote branch
			git push origin topic-a

			# Delete local branch to simulate checking out remote branch for first time
			git checkout main
			git branch -D topic-a
		`)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)

		// Verify topic-a doesn't exist locally
		localBranchExists := strings.TrimSpace(mustExecOutput("sh", "-c", "git branch --list topic-a"))
		assert.Equal(t, localBranchExists, "")

		// Switch to remote branch (first time checkout)
		assert.Equal(t, yascli.Run("branch", "topic-a"), 0)

		// Verify we're on the correct branch
		currentBranch := strings.TrimSpace(mustExecOutput("git", "branch", "--show-current"))
		assert.Equal(t, currentBranch, "topic-a")

		// Verify that gh pr list was called (indicating refresh was initiated)
		cmdLog := mustExecOutput("cat", cmdLogFile)
		assert.Assert(t, strings.Contains(cmdLog, "gh"), "Expected gh command to be called")
		assert.Assert(t, strings.Contains(cmdLog, "pr"), "Expected gh pr command to be called")
		assert.Assert(t, strings.Contains(cmdLog, "list"), "Expected gh pr list to be called")
	})
}
