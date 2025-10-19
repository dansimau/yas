package test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/stringutil"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"github.com/fatih/color"
	"gotest.tools/v3/assert"
)

func TestList_NeedsRestack(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# topic-b
		git checkout -b topic-b
		touch b
		git add b
		git commit -m "topic-b-0"

		# update main (this will cause topic-a to need restack)
		git checkout main
		echo 1 > main
		git add main
		git commit -m "main-1"

		# on branch topic-b
		git checkout topic-b
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())

	// Capture the list output
	result := cli.Run("list")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	// topic-a should show "needs restack" because main has new commits
	assert.Assert(t, strings.Contains(output, "topic-a") && strings.Contains(output, "needs restack"),
		"topic-a should show 'needs restack' but got: %s", output)

	// topic-b should NOT show "needs restack" because topic-a hasn't changed
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "topic-b") {
			assert.Assert(t, !strings.Contains(line, "needs restack"),
				"topic-b should not show 'needs restack' but got: %s", line)
		}
	}
}

func TestList_AfterRestack_NoWarning(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# update main
		git checkout main
		echo 1 > main
		git add main
		git commit -m "main-1"

		git checkout topic-a
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Restack to fix it
	assert.NilError(t, cli.Run("restack").Err())

	// Now list should not show "(needs restack)"
	result := cli.Run("list")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	assert.Assert(t, !strings.Contains(output, "(needs restack)"),
		"After restack, should not show '(needs restack)' but got: %s", output)
}

func TestList_ShowsCurrentBranch(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# topic-b
		git checkout -b topic-b
		touch b
		git add b
		git commit -m "topic-b-0"
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())

	// Should show star on topic-b (current branch)
	result := cli.Run("list")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "topic-b") {
			assert.Assert(t, strings.Contains(line, "*"),
				"topic-b should show '*' (current branch) but got: %s", line)
		}
	}
}

func TestList_ShowsCurrentBranch_OnTrunk(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# back to main
		git checkout main
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Should show star on main (current branch)
	result := cli.Run("list")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	lines := strings.Split(output, "\n")
	foundMainWithStar := false

	for _, line := range lines {
		if strings.Contains(line, "main") && strings.Contains(line, "*") {
			foundMainWithStar = true

			break
		}
	}

	assert.Assert(t, foundMainWithStar,
		"main should show '*' (current branch) but got: %s", output)
}

func TestList_ShowsPRInfo(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Mock gh pr list to return existing PR
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		URL:         "https://github.com/test/test/pull/42",
		BaseRefName: "main",
	})

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		# Set up "remote" repository
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		git push origin topic-a
	`, map[string]string{
		"fakeOrigin": fakeOrigin,
	}))

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Refresh to get PR metadata
	assert.NilError(t, cli.Run("refresh", "topic-a").Err())

	// Capture the list output
	result := cli.Run("list")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	// Verify PR info appears in list
	assert.Assert(t, strings.Contains(output, "topic-a"), "List should contain topic-a")
	assert.Assert(t, strings.Contains(output, "[https://github.com/test/test/pull/42]"),
		"List should show PR URL, but got: %s", output)
}

func TestList_ShowsDraftPR(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Mock gh pr list to return existing draft PR
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		URL:         "https://github.com/test/test/pull/99",
		BaseRefName: "main",
		IsDraft:     true,
	})

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		# Set up "remote" repository
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		git push origin topic-a
	`, map[string]string{
		"fakeOrigin": fakeOrigin,
	}))

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Refresh to get PR metadata
	assert.NilError(t, cli.Run("refresh", "topic-a").Err())

	// Capture the list output
	result := cli.Run("list")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	// Verify draft PR info appears in list
	assert.Assert(t, strings.Contains(output, "topic-a"), "List should contain topic-a")
	assert.Assert(t, strings.Contains(output, "[https://github.com/test/test/pull/99]"),
		"List should show PR URL, but got: %s", output)
}

func TestList_CurrentStack(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# Create stack: main -> topic-a -> topic-b -> topic-c
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

		# Create a sibling branch: main -> topic-x (not in current stack)
		git checkout main
		git checkout -b topic-x
		touch x
		git add x
		git commit -m "topic-x-0"

		# Create a fork from topic-b: topic-b -> topic-d (should be in stack)
		git checkout topic-b
		git checkout -b topic-d
		touch d
		git add d
		git commit -m "topic-d-0"

		# Go to topic-b for testing
		git checkout topic-b
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())
	assert.NilError(t, cli.Run("add", "topic-c", "--parent=topic-b").Err())
	assert.NilError(t, cli.Run("add", "topic-x", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-d", "--parent=topic-b").Err())

	// Full list should show all branches
	fullResult := cli.Run("list")
	assert.NilError(t, fullResult.Err())
	fullOutput := fullResult.Stdout()

	assert.Assert(t, strings.Contains(fullOutput, "topic-a"), "Full list should contain topic-a")
	assert.Assert(t, strings.Contains(fullOutput, "topic-b"), "Full list should contain topic-b")
	assert.Assert(t, strings.Contains(fullOutput, "topic-c"), "Full list should contain topic-c")
	assert.Assert(t, strings.Contains(fullOutput, "topic-x"), "Full list should contain topic-x")
	assert.Assert(t, strings.Contains(fullOutput, "topic-d"), "Full list should contain topic-d")

	// Current stack from topic-b should include:
	// - Ancestors: main, topic-a
	// - Current: topic-b
	// - Descendants: topic-c, topic-d (both children)
	// - Should NOT include: topic-x
	stackResult := cli.Run("list", "--current-stack")
	assert.NilError(t, stackResult.Err())
	stackOutput := stackResult.Stdout()

	assert.Assert(t, strings.Contains(stackOutput, "main"), "Current stack should contain main")
	assert.Assert(t, strings.Contains(stackOutput, "topic-a"), "Current stack should contain topic-a")
	assert.Assert(t, strings.Contains(stackOutput, "topic-b"), "Current stack should contain topic-b")
	assert.Assert(t, strings.Contains(stackOutput, "topic-c"), "Current stack should contain topic-c (descendant)")
	assert.Assert(t, strings.Contains(stackOutput, "topic-d"), "Current stack should contain topic-d (descendant fork)")
	assert.Assert(t, !strings.Contains(stackOutput, "topic-x"), "Current stack should NOT contain topic-x (sibling branch)")
}

func TestList_GreysOutBranchPrefix(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Use FORCE_COLOR to enable colors in the subprocess (fatih/color respects this)
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("FORCE_COLOR", "1"),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# user/topic-a
		git checkout -b user/topic-a
		touch a
		git add a
		git commit -m "topic-a-0"
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "user/topic-a", "--parent=main").Err())

	result := cli.Run("list")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	// Build the expected colored prefix (grey "user/")
	greyPrefix := color.New(color.FgHiBlack).Sprint("user/")
	assert.Assert(t, strings.Contains(output, greyPrefix+"topic-a"),
		"List should grey out branch prefix but got: %s", output)
}

func TestList_ShowsNeedsSubmit_WhenBaseRefDiffers(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Mock an existing PR with base branch topic-a (but local parent is main)
	mockGitHubPRForBranch(cli, "topic-b", yas.PullRequestMetadata{
		URL:         "https://github.com/test/test/pull/42",
		BaseRefName: "topic-a", // PR targets topic-a
	})

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		# Set up "remote" repository
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# topic-b
		git checkout -b topic-b
		touch b
		git add b
		git commit -m "topic-b-0"

		# Push topic-b to remote so hashes match
		git push origin topic-b

		git checkout main
	`, map[string]string{
		"fakeOrigin": fakeOrigin,
	}))

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Set topic-b's parent to main (simulating restack after topic-a merged)
	// But PR still targets topic-a
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=main").Err())

	// Refresh remote status to get PR metadata
	assert.NilError(t, cli.Run("refresh", "topic-b").Err())

	// Capture the list output
	result := cli.Run("list")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	// topic-b should show "(needs submit)" because base differs from parent
	assert.Assert(t, strings.Contains(output, "topic-b") && strings.Contains(output, "(needs submit)"),
		"topic-b should show '(needs submit)' when base differs from parent, but got: %s", output)
}

func TestList_ShowsBothNeedsRestackAndNeedsSubmit(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Mock an existing PR
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		URL:         "https://github.com/test/test/pull/42",
		BaseRefName: "main",
	})

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		# Set up "remote" repository
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# Push to remote
		git push origin topic-a

		# Update main (causes need for restack)
		git checkout main
		echo 1 > main
		git add main
		git commit -m "main-1"

		# Make local change to topic-a (causes need for submit)
		git checkout topic-a
		echo 2 > a
		git add a
		git commit -m "topic-a-1"
	`, map[string]string{
		"fakeOrigin": fakeOrigin,
	}))

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Refresh remote status to get PR metadata
	assert.NilError(t, cli.Run("refresh", "topic-a").Err())

	// Capture the list output
	result := cli.Run("list")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	// topic-a should show both warnings
	assert.Assert(t, strings.Contains(output, "topic-a") &&
		strings.Contains(output, "needs restack") &&
		strings.Contains(output, "needs submit"),
		"topic-a should show '(needs restack, needs submit)' but got: %s", output)
}

func TestList_ShowsNotSubmitted_WhenNoPRExists(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a (not submitted yet)
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		git checkout main
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Capture the list output
	result := cli.Run("list")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	// topic-a should show "(not submitted)" because it has no PR
	assert.Assert(t, strings.Contains(output, "topic-a") && strings.Contains(output, "(not submitted)"),
		"topic-a should show '(not submitted)' when no PR exists, but got: %s", output)
}

func TestList_ShowsNeedsRestackAndNotSubmitted(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# Update main (causes need for restack)
		git checkout main
		echo 1 > main
		git add main
		git commit -m "main-1"

		git checkout topic-a
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Capture the list output
	result := cli.Run("list")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	// topic-a should show both warnings
	assert.Assert(t, strings.Contains(output, "topic-a") &&
		strings.Contains(output, "needs restack") &&
		strings.Contains(output, "not submitted"),
		"topic-a should show '(needs restack, not submitted)' but got: %s", output)
}

func TestList_SortsByCreatedTimestamp(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# Create branches (will be tracked in specific order to test timestamp sorting)
		git checkout -b topic-c
		touch c
		git add c
		git commit -m "topic-c-0"

		git checkout main
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		git checkout main
		git checkout -b topic-b
		touch b
		git add b
		git commit -m "topic-b-0"

		git checkout main
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())

	// Add branches in the order: topic-c first, then topic-a, then topic-b
	// This establishes the Created timestamps
	assert.NilError(t, cli.Run("add", "topic-c", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=main").Err())

	// Capture the list output
	result := cli.Run("list")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	// Find the line positions of each branch
	lines := strings.Split(output, "\n")
	topicCPos := -1
	topicAPos := -1
	topicBPos := -1

	for i, line := range lines {
		if strings.Contains(line, "topic-c") {
			topicCPos = i
		}

		if strings.Contains(line, "topic-a") {
			topicAPos = i
		}

		if strings.Contains(line, "topic-b") {
			topicBPos = i
		}
	}

	// Verify all branches were found
	assert.Assert(t, topicCPos != -1, "topic-c should appear in list")
	assert.Assert(t, topicAPos != -1, "topic-a should appear in list")
	assert.Assert(t, topicBPos != -1, "topic-b should appear in list")

	// Verify they appear in timestamp order (oldest first: topic-c, topic-a, topic-b)
	assert.Assert(t, topicCPos < topicAPos,
		"topic-c (created first) should appear before topic-a, but got positions: c=%d, a=%d", topicCPos, topicAPos)
	assert.Assert(t, topicAPos < topicBPos,
		"topic-a (created second) should appear before topic-b, but got positions: a=%d, b=%d", topicAPos, topicBPos)
}

func TestList_HidesManuallyDeletedLeafBranch(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# topic-b
		git checkout -b topic-b
		touch b
		git add b
		git commit -m "topic-b-0"

		git checkout main
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())

	// Mark topic-b as deleted in yas metadata (simulating yas delete)
	state := readStateFileFromDir(t, tempDir)
	branch := state.Branches["topic-b"]
	now := time.Now()
	branch.Deleted = &now
	state.Branches["topic-b"] = branch
	writeStateFileToDir(t, tempDir, state)

	// Also delete from git to match the real scenario
	testutil.ExecOrFail(t, tempDir, "git branch -D topic-b")

	// Capture the list output
	result := cli.Run("list")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	// topic-b should NOT appear (deleted leaf branch is hidden)
	assert.Assert(t, !strings.Contains(output, "topic-b"),
		"topic-b (deleted leaf) should NOT appear in list, but got: %s", output)

	// topic-a should still appear
	assert.Assert(t, strings.Contains(output, "topic-a"),
		"topic-a should still appear in list, but got: %s", output)
}

func TestList_ShowsManuallyDeletedBranchWithLivingChild(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# topic-b
		git checkout -b topic-b
		touch b
		git add b
		git commit -m "topic-b-0"

		git checkout main
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())

	// Delete topic-a from git (but keep in yas metadata and keep topic-b)
	testutil.ExecOrFail(t, tempDir, "git branch -D topic-a")

	// Capture the list output
	result := cli.Run("list")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	// topic-a SHOULD appear because it has a living child (topic-b)
	assert.Assert(t, strings.Contains(output, "topic-a"),
		"topic-a (deleted with living child) SHOULD appear in list, but got: %s", output)

	// topic-a should show "(deleted)" status
	lines := strings.Split(output, "\n")
	foundTopicADeleted := false

	for _, line := range lines {
		if strings.Contains(line, "topic-a") && strings.Contains(line, "deleted") {
			foundTopicADeleted = true

			break
		}
	}

	assert.Assert(t, foundTopicADeleted,
		"topic-a should show '(deleted)' status, but got: %s", output)

	// topic-b should still appear as child of topic-a
	assert.Assert(t, strings.Contains(output, "topic-b"),
		"topic-b should still appear in list, but got: %s", output)
}

func TestList_HidesManuallyDeletedSubtreeRecursively(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# topic-b
		git checkout -b topic-b
		touch b
		git add b
		git commit -m "topic-b-0"

		# topic-c
		git checkout -b topic-c
		touch c
		git add c
		git commit -m "topic-c-0"

		git checkout main
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())
	assert.NilError(t, cli.Run("add", "topic-c", "--parent=topic-b").Err())

	// Mark all branches as deleted in yas metadata (simulating yas delete)
	state := readStateFileFromDir(t, tempDir)
	now := time.Now()
	branchA := state.Branches["topic-a"]
	branchA.Deleted = &now
	state.Branches["topic-a"] = branchA
	branchB := state.Branches["topic-b"]
	branchB.Deleted = &now
	state.Branches["topic-b"] = branchB
	branchC := state.Branches["topic-c"]
	branchC.Deleted = &now
	state.Branches["topic-c"] = branchC
	writeStateFileToDir(t, tempDir, state)

	// Also delete from git to match the real scenario
	testutil.ExecOrFail(t, tempDir, `
		git branch -D topic-a
		git branch -D topic-b
		git branch -D topic-c
	`)

	// Capture the list output
	result := cli.Run("list")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	// None of the deleted branches should appear (entire subtree is hidden)
	assert.Assert(t, !strings.Contains(output, "topic-a"),
		"topic-a (deleted subtree) should NOT appear in list, but got: %s", output)
	assert.Assert(t, !strings.Contains(output, "topic-b"),
		"topic-b (deleted subtree) should NOT appear in list, but got: %s", output)
	assert.Assert(t, !strings.Contains(output, "topic-c"),
		"topic-c (deleted subtree) should NOT appear in list, but got: %s", output)

	// Only main should appear
	assert.Assert(t, strings.Contains(output, "main"),
		"main should still appear in list, but got: %s", output)
}

func TestList_HidesDeletedLeafBranch(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# topic-b
		git checkout -b topic-b
		touch b
		git add b
		git commit -m "topic-b-0"

		git checkout main
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())

	// Mark topic-b as deleted in yas metadata (but keep in git)
	state := readStateFileFromDir(t, tempDir)
	branch := state.Branches["topic-b"]
	now := time.Now()
	branch.Deleted = &now
	state.Branches["topic-b"] = branch
	writeStateFileToDir(t, tempDir, state)

	// Capture the list output
	result := cli.Run("list")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	// topic-b should NOT appear (marked as deleted in metadata)
	assert.Assert(t, !strings.Contains(output, "topic-b"),
		"topic-b (deleted in metadata) should NOT appear in list, but got: %s", output)

	// topic-a should still appear
	assert.Assert(t, strings.Contains(output, "topic-a"),
		"topic-a should still appear in list, but got: %s", output)
}

func TestList_FromInsideWorktree(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	worktreePath := filepath.Join(tempDir, "worktrees", "feature-a")

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create main repo with branches and worktree
	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"

		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		git checkout -b feature-a
		touch a
		git add a
		git commit -m "feature-a-0"

		git checkout -b feature-b
		touch b
		git add b
		git commit -m "feature-b-0"

		git checkout main
		mkdir -p `+filepath.Dir(worktreePath)+`
		git worktree add `+worktreePath+` feature-a
	`)

	// Initialize yas from main repo
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "feature-b", "--parent=feature-a").Err())

	// Run 'yas ls' from inside the worktree
	cliInWorktree := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(worktreePath),
	)

	result := cliInWorktree.Run("list")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	// Verify feature-a is marked as current branch (with *)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "feature-a") {
			assert.Assert(t, strings.Contains(line, "*"),
				"feature-a should show '*' (current branch) when run from worktree, got: %s", line)
		}

		if strings.Contains(line, "feature-b") {
			assert.Assert(t, !strings.Contains(line, "*"),
				"feature-b should NOT show '*' when run from worktree, got: %s", line)
		}
	}
}

func TestList_All_ShowsUntrackedBranches(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# Create a tracked branch
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# Create an untracked branch
		git checkout main
		git checkout -b untracked-b
		touch b
		git add b
		git commit -m "untracked-b-0"

		git checkout main
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Without --all, should not show untracked branch
	result := cli.Run("list")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	assert.Assert(t, strings.Contains(output, "topic-a"), "List should contain topic-a")
	assert.Assert(t, !strings.Contains(output, "untracked-b"), "List should not contain untracked-b without --all")

	// With --all, should show untracked branch
	resultAll := cli.Run("list", "--all")
	assert.NilError(t, resultAll.Err())
	outputAll := resultAll.Stdout()

	assert.Assert(t, strings.Contains(outputAll, "topic-a"), "List --all should contain topic-a")
	assert.Assert(t, strings.Contains(outputAll, "untracked-b"), "List --all should contain untracked-b")
}

func TestList_All_GreysOutUntrackedBranches(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# Create a tracked branch
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# Create an untracked branch
		git checkout main
		git checkout -b untracked-b
		touch b
		git add b
		git commit -m "untracked-b-0"

		git checkout main
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	result := cli.Run("list", "--all")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	// Check that untracked-b is shown (tracked branches have status like "(not submitted)"
	// but untracked branches appear without status indicators)
	assert.Assert(t, strings.Contains(output, "untracked-b"),
		"List --all should show untracked-b but got: %s", output)

	// Verify untracked branch doesn't have status like "(not submitted)" - that's for tracked branches
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "untracked-b") {
			// Untracked branches should NOT have status indicators like "(not submitted)"
			assert.Assert(t, !strings.Contains(line, "(not submitted)"),
				"Untracked branch should not show '(not submitted)' status but got: %s", line)

			break
		}
	}
}

func TestList_All_PlacesUntrackedBranchesInHierarchy(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# Create a tracked branch
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# Create an untracked branch from topic-a
		git checkout -b untracked-child
		touch child
		git add child
		git commit -m "untracked-child-0"

		git checkout main
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	result := cli.Run("list", "--all")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	// Find the line positions to verify hierarchy
	lines := strings.Split(output, "\n")
	topicAPos := -1
	untrackedChildPos := -1

	for i, line := range lines {
		if strings.Contains(line, "topic-a") {
			topicAPos = i
		}

		if strings.Contains(line, "untracked-child") {
			untrackedChildPos = i
		}
	}

	assert.Assert(t, topicAPos != -1, "topic-a should appear in list")
	assert.Assert(t, untrackedChildPos != -1, "untracked-child should appear in list")

	// untracked-child should come after topic-a (its parent)
	assert.Assert(t, untrackedChildPos > topicAPos,
		"untracked-child should appear after topic-a (its parent), but got positions: topic-a=%d, untracked-child=%d", topicAPos, untrackedChildPos)
}
