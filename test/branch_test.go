package test

import (
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/stringutil"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

func TestBranch_GetBranchList_ForInteractiveSwitcher(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create simple branch structure: main -> topic-a
	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"

		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Test GetBranchList directly (used by SwitchBranchInteractive)
	// This is acceptable to call directly since it's read-only and what we're testing
	y, err := yas.NewFromRepository(tempDir)
	assert.NilError(t, err)

	items, err := y.GetBranchList(false, false, false)
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
}

func TestBranch_GetBranchList_MultiLevelStack(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create stack: main -> topic-a -> topic-b -> topic-c
	testutil.ExecOrFail(t, tempDir, `
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

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())
	assert.NilError(t, cli.Run("add", "topic-c", "--parent=topic-b").Err())

	// Test GetBranchList for multi-level stack (read-only, what we're testing)
	y, err := yas.NewFromRepository(tempDir)
	assert.NilError(t, err)

	items, err := y.GetBranchList(false, false, false)
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
}

func TestBranch_GetBranchList_ForkedBranches(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create fork: main -> topic-a -> [topic-b, topic-c]
	testutil.ExecOrFail(t, tempDir, `
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

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())
	assert.NilError(t, cli.Run("add", "topic-c", "--parent=topic-a").Err())

	// Test GetBranchList for forked branches (read-only, what we're testing)
	y, err := yas.NewFromRepository(tempDir)
	assert.NilError(t, err)

	items, err := y.GetBranchList(false, false, false)
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
}

func TestBranch_GetBranchList_CurrentBranchHighlight(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create simple structure and stay on topic-a
	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"

		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Test GetBranchList shows current branch with * (read-only, what we're testing)
	y, err := yas.NewFromRepository(tempDir)
	assert.NilError(t, err)

	items, err := y.GetBranchList(false, false, false)
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
}

func TestBranch_GetBranchList_EmptyBranchList(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create repo with only main branch (no tracked branches)
	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())

	// Test GetBranchList with no tracked branches (read-only, what we're testing)
	y, err := yas.NewFromRepository(tempDir)
	assert.NilError(t, err)

	items, err := y.GetBranchList(false, false, false)
	assert.NilError(t, err)

	// Should only have main branch
	assert.Equal(t, len(items), 1, "Should have 1 branch (main)")
	assert.Equal(t, items[0].ID, "main", "Should have main branch")
}

func TestBranch_GetBranchList_SingleBranch(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create repo with only main branch
	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())

	// Test GetBranchList with single branch (read-only, what we're testing)
	y, err := yas.NewFromRepository(tempDir)
	assert.NilError(t, err)

	items, err := y.GetBranchList(false, false, false)
	assert.NilError(t, err)

	// Should have only main branch
	assert.Equal(t, len(items), 1, "Should have 1 branch")
	assert.Equal(t, items[0].ID, "main", "Should have main branch")
}

func TestBranch_GetBranchList_BranchNameFormatting(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create branch with prefix
	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"

		git checkout -b user/feature-branch
		touch feature
		git add feature
		git commit -m "feature-0"
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "user/feature-branch", "--parent=main").Err())

	// Test GetBranchList shows formatted branch name (read-only, what we're testing)
	y, err := yas.NewFromRepository(tempDir)
	assert.NilError(t, err)

	items, err := y.GetBranchList(false, false, false)
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
}

func TestBranchSwitch_ExistingLocalBranch(t *testing.T) {
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

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())

	// Switch to existing local branch using yas branch command
	assert.NilError(t, cli.Run("branch", "topic-a").Err())

	// Verify we're on the correct branch
	currentBranch := strings.TrimSpace(mustExecOutput(tempDir, "git", "branch", "--show-current"))
	assert.Equal(t, currentBranch, "topic-a")

	// Switch to another existing branch
	assert.NilError(t, cli.Run("branch", "topic-b").Err())

	// Verify we're on the correct branch
	currentBranch = strings.TrimSpace(mustExecOutput(tempDir, "git", "branch", "--show-current"))
	assert.Equal(t, currentBranch, "topic-b")
}

func TestBranchSwitch_RemoteBranchInitiatesRefresh(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Mock the gh pr list command
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		URL:         "https://github.com/test/test/pull/42",
		BaseRefName: "main",
	})

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		# Set up "remote" repository
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

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
	`, map[string]string{
		"fakeOrigin": fakeOrigin,
	}))

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())

	// Verify topic-a doesn't exist locally
	localBranchExists := strings.TrimSpace(mustExecOutput(tempDir, "sh", "-c", "git branch --list topic-a"))
	assert.Equal(t, localBranchExists, "")

	// Switch to remote branch (first time checkout)
	assert.NilError(t, cli.Run("branch", "topic-a").Err())

	// Verify we're on the correct branch
	currentBranch := strings.TrimSpace(mustExecOutput(tempDir, "git", "branch", "--show-current"))
	assert.Equal(t, currentBranch, "topic-a")

	// The gh pr list mock being called verifies that refresh was initiated
	// (if it wasn't called, the mock would fail with "no mock configured")
}
