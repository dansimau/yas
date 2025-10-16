package test

import (
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"github.com/dansimau/yas/pkg/yascli"
	"github.com/fatih/color"
	"gotest.tools/v3/assert"
)

func TestList_NeedsRestack(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
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

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-b", "--parent=topic-a"), 0)

		// Capture the list output
		output := captureStdout(t, func() {
			assert.Equal(t, yascli.Run("list"), 0)
		})

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
	})
}

func TestList_AfterRestack_NoWarning(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
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

			# update main
			git checkout main
			echo 1 > main
			git add main
			git commit -m "main-1"

			git checkout topic-a
		`)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)

		// Restack to fix it
		assert.Equal(t, yascli.Run("restack"), 0)

		// Now list should not show "(needs restack)"
		output := captureStdout(t, func() {
			assert.Equal(t, yascli.Run("list"), 0)
		})

		assert.Assert(t, !strings.Contains(output, "(needs restack)"),
			"After restack, should not show '(needs restack)' but got: %s", output)
	})
}

func TestList_ShowsCurrentBranch(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
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

			# topic-b
			git checkout -b topic-b
			touch b
			git add b
			git commit -m "topic-b-0"
		`)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-b", "--parent=topic-a"), 0)

		// Should show star on topic-b (current branch)
		output := captureStdout(t, func() {
			assert.Equal(t, yascli.Run("list"), 0)
		})

		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if strings.Contains(line, "topic-b") {
				assert.Assert(t, strings.Contains(line, "*"),
					"topic-b should show '*' (current branch) but got: %s", line)
			}
		}
	})
}

func TestList_ShowsCurrentBranch_OnTrunk(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
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

			# back to main
			git checkout main
		`)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)

		// Should show star on main (current branch)
		output := captureStdout(t, func() {
			assert.Equal(t, yascli.Run("list"), 0)
		})

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
	})
}

func TestList_ShowsPRInfo(t *testing.T) {
	_, cleanup := setupMockCommandsWithPR(t, mockPROptions{
		ID:      "PR_kwDOTest123",
		State:   "OPEN",
		URL:     "https://github.com/test/test/pull/42",
		IsDraft: false,
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
		`)

		// Initialize yas config
		cfg := yas.Config{
			RepoDirectory: ".",
			TrunkBranch:   "main",
		}
		_, err := yas.WriteConfig(cfg)
		assert.NilError(t, err)

		// Create YAS instance and track branch
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)
		err = y.SetParent("topic-a", "main", "")
		assert.NilError(t, err)

		// Submit to create PR and populate metadata
		err = y.Submit()
		assert.NilError(t, err)

		// Capture the list output
		output := captureStdout(t, func() {
			assert.Equal(t, yascli.Run("list"), 0)
		})

		// Verify PR info appears in list
		assert.Assert(t, strings.Contains(output, "topic-a"), "List should contain topic-a")
		assert.Assert(t, strings.Contains(output, "[https://github.com/test/test/pull/42]"),
			"List should show PR URL, but got: %s", output)
	})
}

func TestList_ShowsDraftPR(t *testing.T) {
	_, cleanup := setupMockCommandsWithPR(t, mockPROptions{
		ID:      "PR_kwDOTest456",
		State:   "OPEN",
		URL:     "https://github.com/test/test/pull/99",
		IsDraft: true,
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
		`)

		// Initialize yas config
		cfg := yas.Config{
			RepoDirectory: ".",
			TrunkBranch:   "main",
		}
		_, err := yas.WriteConfig(cfg)
		assert.NilError(t, err)

		// Create YAS instance and track branch
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)
		err = y.SetParent("topic-a", "main", "")
		assert.NilError(t, err)

		// Submit to create PR and populate metadata
		err = y.Submit()
		assert.NilError(t, err)

		// Capture the list output
		output := captureStdout(t, func() {
			assert.Equal(t, yascli.Run("list"), 0)
		})

		// Verify draft PR info appears in list
		assert.Assert(t, strings.Contains(output, "topic-a"), "List should contain topic-a")
		assert.Assert(t, strings.Contains(output, "[https://github.com/test/test/pull/99]"),
			"List should show PR URL, but got: %s", output)
	})
}

func TestList_CurrentStack(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main

			# main
			touch main
			git add main
			git commit -m "main-0"

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

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-b", "--parent=topic-a"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-c", "--parent=topic-b"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-x", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-d", "--parent=topic-b"), 0)

		// Full list should show all branches
		fullOutput := captureStdout(t, func() {
			assert.Equal(t, yascli.Run("list"), 0)
		})

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
		stackOutput := captureStdout(t, func() {
			assert.Equal(t, yascli.Run("list", "--current-stack"), 0)
		})

		assert.Assert(t, strings.Contains(stackOutput, "main"), "Current stack should contain main")
		assert.Assert(t, strings.Contains(stackOutput, "topic-a"), "Current stack should contain topic-a")
		assert.Assert(t, strings.Contains(stackOutput, "topic-b"), "Current stack should contain topic-b")
		assert.Assert(t, strings.Contains(stackOutput, "topic-c"), "Current stack should contain topic-c (descendant)")
		assert.Assert(t, strings.Contains(stackOutput, "topic-d"), "Current stack should contain topic-d (descendant fork)")
		assert.Assert(t, !strings.Contains(stackOutput, "topic-x"), "Current stack should NOT contain topic-x (sibling branch)")
	})
}

func TestList_GreysOutBranchPrefix(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
                        git init --initial-branch=main

                        # main
                        touch main
                        git add main
                        git commit -m "main-0"

                        # user/topic-a
                        git checkout -b user/topic-a
                        touch a
                        git add a
                        git commit -m "topic-a-0"
                `)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=user/topic-a", "--parent=main"), 0)

		previousNoColor := color.NoColor
		color.NoColor = false

		defer func() { color.NoColor = previousNoColor }()

		output := captureStdout(t, func() {
			assert.Equal(t, yascli.Run("list"), 0)
		})

		greyPrefix := color.New(color.FgHiBlack).Sprint("user/")
		assert.Assert(t, strings.Contains(output, greyPrefix+"topic-a"),
			"List should grey out branch prefix but got: %s", output)
	})
}

func TestList_ShowsNeedsSubmit_WhenBaseRefDiffers(t *testing.T) {
	// Mock an existing PR with base branch topic-a (but local parent is main)
	_, cleanup := setupMockCommandsWithPR(t, mockPROptions{
		ID:          "PR_kwDOTest123",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/42",
		IsDraft:     false,
		BaseRefName: "topic-a", // PR targets topic-a
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

			# topic-b
			git checkout -b topic-b
			touch b
			git add b
			git commit -m "topic-b-0"

			# Push topic-b to remote so hashes match
			git push origin topic-b

			git checkout main
		`)

		// Initialize yas config
		cfg := yas.Config{
			RepoDirectory: ".",
			TrunkBranch:   "main",
		}
		_, err := yas.WriteConfig(cfg)
		assert.NilError(t, err)

		// Create YAS instance
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)

		// Track branches
		err = y.SetParent("topic-a", "main", "")
		assert.NilError(t, err)

		// Set topic-b's parent to main (simulating restack after topic-a merged)
		// But PR still targets topic-a
		err = y.SetParent("topic-b", "main", "")
		assert.NilError(t, err)

		// Refresh remote status to get PR metadata
		err = y.RefreshRemoteStatus("topic-b")
		assert.NilError(t, err)

		// Capture the list output
		output := captureStdout(t, func() {
			assert.Equal(t, yascli.Run("list"), 0)
		})

		// topic-b should show "(needs submit)" because base differs from parent
		assert.Assert(t, strings.Contains(output, "topic-b") && strings.Contains(output, "(needs submit)"),
			"topic-b should show '(needs submit)' when base differs from parent, but got: %s", output)
	})
}

func TestList_ShowsBothNeedsRestackAndNeedsSubmit(t *testing.T) {
	// Mock an existing PR
	_, cleanup := setupMockCommandsWithPR(t, mockPROptions{
		ID:          "PR_kwDOTest123",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/42",
		IsDraft:     false,
		BaseRefName: "main",
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
		`)

		// Initialize yas config
		cfg := yas.Config{
			RepoDirectory: ".",
			TrunkBranch:   "main",
		}
		_, err := yas.WriteConfig(cfg)
		assert.NilError(t, err)

		// Create YAS instance
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)

		// Track topic-a
		err = y.SetParent("topic-a", "main", "")
		assert.NilError(t, err)

		// Refresh remote status to get PR metadata
		err = y.RefreshRemoteStatus("topic-a")
		assert.NilError(t, err)

		// Capture the list output
		output := captureStdout(t, func() {
			assert.Equal(t, yascli.Run("list"), 0)
		})

		// topic-a should show both warnings
		assert.Assert(t, strings.Contains(output, "topic-a") &&
			strings.Contains(output, "needs restack") &&
			strings.Contains(output, "needs submit"),
			"topic-a should show '(needs restack, needs submit)' but got: %s", output)
	})
}

func TestList_ShowsNotSubmitted_WhenNoPRExists(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main

			# main
			touch main
			git add main
			git commit -m "main-0"

			# topic-a (not submitted yet)
			git checkout -b topic-a
			touch a
			git add a
			git commit -m "topic-a-0"

			git checkout main
		`)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)

		// Capture the list output
		output := captureStdout(t, func() {
			assert.Equal(t, yascli.Run("list"), 0)
		})

		// topic-a should show "(not submitted)" because it has no PR
		assert.Assert(t, strings.Contains(output, "topic-a") && strings.Contains(output, "(not submitted)"),
			"topic-a should show '(not submitted)' when no PR exists, but got: %s", output)
	})
}

func TestList_ShowsNeedsRestackAndNotSubmitted(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
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

			# Update main (causes need for restack)
			git checkout main
			echo 1 > main
			git add main
			git commit -m "main-1"

			git checkout topic-a
		`)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)

		// Capture the list output
		output := captureStdout(t, func() {
			assert.Equal(t, yascli.Run("list"), 0)
		})

		// topic-a should show both warnings
		assert.Assert(t, strings.Contains(output, "topic-a") &&
			strings.Contains(output, "needs restack") &&
			strings.Contains(output, "not submitted"),
			"topic-a should show '(needs restack, not submitted)' but got: %s", output)
	})
}

func TestList_SortsByCreatedTimestamp(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main

			# main
			touch main
			git add main
			git commit -m "main-0"

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

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)

		// Add branches in the order: topic-c first, then topic-a, then topic-b
		// This establishes the Created timestamps
		assert.Equal(t, yascli.Run("add", "--branch=topic-c", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-b", "--parent=main"), 0)

		// Capture the list output
		output := captureStdout(t, func() {
			assert.Equal(t, yascli.Run("list"), 0)
		})

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
	})
}
