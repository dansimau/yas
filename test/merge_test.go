package test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

func TestMerge_FailsWhenNoRestackInProgress(t *testing.T) {
	_, cleanup := setupMockCommandsWithPR(t, mockPROptions{
		ID:                "PR_kwDOTest123",
		State:             "OPEN",
		URL:               "https://github.com/test/test/pull/42",
		BaseRefName:       "main",
		ReviewDecision:    "APPROVED",
		StatusCheckRollup: "[]", // Empty array means SUCCESS
	})
	defer cleanup()

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git remote add origin https://fake.origin/test/test.git

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

		// Simulate a restack in progress
		restackState := &yas.RestackState{
			StartingBranch: "topic-a",
			CurrentBranch:  "topic-a",
			CurrentParent:  "main",
		}
		err = yas.SaveRestackState(".", restackState)
		assert.NilError(t, err)

		// Try to merge - should fail
		err = y.Merge(false)
		assert.ErrorContains(t, err, "restack operation is already in progress")
	})
}

func TestMerge_FailsWhenNoPR(t *testing.T) {
	_, cleanup := setupMockCommands(t, "") // No existing PR
	defer cleanup()

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git remote add origin https://fake.origin/test/test.git

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

		// Try to merge - should fail
		err = y.Merge(false)
		assert.ErrorContains(t, err, "does not have a PR")
	})
}

func TestMerge_FailsWhenNotAtTopOfStack(t *testing.T) {
	_, cleanup := setupMockCommandsWithPR(t, mockPROptions{
		ID:                "PR_kwDOTest123",
		State:             "OPEN",
		URL:               "https://github.com/test/test/pull/42",
		BaseRefName:       "topic-a", // Parent is topic-a, not main
		ReviewDecision:    "APPROVED",
		StatusCheckRollup: "[]",
	})
	defer cleanup()

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git remote add origin https://fake.origin/test/test.git

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

			# topic-b (child of topic-a)
			git checkout -b topic-b
			touch b
			git add b
			git commit -m "topic-b-0"
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

		// Refresh to get PR metadata
		err = y.RefreshRemoteStatus("topic-b")
		assert.NilError(t, err)

		// Try to merge topic-b (which is not at top of stack) - should fail
		testutil.ExecOrFail(t, "git checkout topic-b")

		err = y.Merge(false)
		assert.ErrorContains(t, err, "must be at top of stack")
		assert.ErrorContains(t, err, "Merge parent branches first")
	})
}

func TestMerge_FailsWhenNeedsRestack(t *testing.T) {
	_, cleanup := setupMockCommandsWithPR(t, mockPROptions{
		ID:                "PR_kwDOTest123",
		State:             "OPEN",
		URL:               "https://github.com/test/test/pull/42",
		BaseRefName:       "main",
		ReviewDecision:    "APPROVED",
		StatusCheckRollup: "[]",
	})
	defer cleanup()

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git remote add origin https://fake.origin/test/test.git

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

			# Add a commit to main (will make topic-a need restack)
			git checkout main
			touch main2
			git add main2
			git commit -m "main-1"
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

		// Refresh to get PR metadata
		err = y.RefreshRemoteStatus("topic-a")
		assert.NilError(t, err)

		// Try to merge - should fail because branch needs restack
		testutil.ExecOrFail(t, "git checkout topic-a")

		err = y.Merge(false)
		assert.ErrorContains(t, err, "branch needs restack")
	})
}

func TestMerge_FailsWhenCINotPassing(t *testing.T) {
	_, cleanup := setupMockCommandsWithPR(t, mockPROptions{
		ID:                "PR_kwDOTest123",
		State:             "OPEN",
		URL:               "https://github.com/test/test/pull/42",
		BaseRefName:       "main",
		ReviewDecision:    "APPROVED",
		StatusCheckRollup: `[{"state":"FAILURE","conclusion":"FAILURE"}]`,
	})
	defer cleanup()

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git remote add origin https://fake.origin/test/test.git

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

		// Submit first to simulate that PR exists and is up to date
		err = y.Submit(false)
		assert.NilError(t, err)

		// Reload instance to get updated metadata
		y, err = yas.NewFromRepository(".")
		assert.NilError(t, err)

		// Refresh PR status to get review/CI info (submit only refreshes basic info)
		err = y.RefreshPRStatus("topic-a")
		assert.NilError(t, err)

		// Try to merge - should fail because CI is failing
		testutil.ExecOrFail(t, "git checkout topic-a")

		err = y.Merge(false)
		assert.ErrorContains(t, err, "CI checks are not passing")
		assert.ErrorContains(t, err, "Use --force to override")
	})
}

func TestMerge_FailsWhenNotApproved(t *testing.T) {
	_, cleanup := setupMockCommandsWithPR(t, mockPROptions{
		ID:                "PR_kwDOTest123",
		State:             "OPEN",
		URL:               "https://github.com/test/test/pull/42",
		BaseRefName:       "main",
		ReviewDecision:    "REVIEW_REQUIRED",
		StatusCheckRollup: "[]",
	})
	defer cleanup()

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git remote add origin https://fake.origin/test/test.git

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

		// Submit first to simulate that PR exists and is up to date
		err = y.Submit(false)
		assert.NilError(t, err)

		// Reload instance to get updated metadata
		y, err = yas.NewFromRepository(".")
		assert.NilError(t, err)

		// Refresh PR status to get review/CI info (submit only refreshes basic info)
		err = y.RefreshPRStatus("topic-a")
		assert.NilError(t, err)

		// Try to merge - should fail because PR needs approval
		testutil.ExecOrFail(t, "git checkout topic-a")

		err = y.Merge(false)
		assert.ErrorContains(t, err, "PR needs approval")
		assert.ErrorContains(t, err, "Use --force to override")
	})
}

func TestMerge_SucceedsWithForceFlag(t *testing.T) {
	cmdLogFile, cleanup := setupMockCommandsWithPR(t, mockPROptions{
		ID:                "PR_kwDOTest123",
		State:             "OPEN",
		URL:               "https://github.com/test/test/pull/42",
		BaseRefName:       "main",
		ReviewDecision:    "REVIEW_REQUIRED",                              // Not approved
		StatusCheckRollup: `[{"state":"FAILURE","conclusion":"FAILURE"}]`, // CI failing
	})
	defer cleanup()

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git remote add origin https://fake.origin/test/test.git

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

		// Submit first to simulate that PR exists and is up to date
		err = y.Submit(false)
		assert.NilError(t, err)

		// Set EDITOR to a script that just adds a comment to the merge message
		editorScript := filepath.Join(t.TempDir(), "editor.sh")
		err = os.WriteFile(editorScript, []byte(`#!/bin/bash
echo "# User edited merge message" >> "$1"
`), 0o755)
		assert.NilError(t, err)
		t.Setenv("EDITOR", editorScript)

		// Try to merge with --force - should succeed even with failing CI/reviews
		testutil.ExecOrFail(t, "git checkout topic-a")

		err = y.Merge(true)
		assert.NilError(t, err)

		// Parse the command log
		commands, err := parseCmdLog(cmdLogFile)
		assert.NilError(t, err)

		// Verify gh pr merge was called
		mergeCmd := findCommand(commands, "gh", "pr", "merge")
		assert.Assert(t, mergeCmd != nil, "gh pr merge should be called")

		// Verify it includes expected flags
		if mergeCmd != nil {
			assert.Assert(t, contains(mergeCmd, "--squash"), "should include --squash")
			// assert.Assert(t, contains(mergeCmd, "--delete-branch"), "should include --delete-branch")
			assert.Assert(t, contains(mergeCmd, "--auto"), "should include --auto")
			assert.Assert(t, contains(mergeCmd, "--subject"), "should include --subject")
			assert.Assert(t, contains(mergeCmd, "--body"), "should include --body")
		}

		// Verify merge message file was cleaned up
		mergeFilePath := filepath.Join(".", ".git", "yas-merge-msg")
		_, err = os.Stat(mergeFilePath)
		assert.Assert(t, os.IsNotExist(err), "merge message file should be cleaned up")
	})
}

func TestMerge_AbortsWhenMergeMessageEmpty(t *testing.T) {
	_, cleanup := setupMockCommandsWithPR(t, mockPROptions{
		ID:                "PR_kwDOTest123",
		State:             "OPEN",
		URL:               "https://github.com/test/test/pull/42",
		BaseRefName:       "main",
		ReviewDecision:    "APPROVED",
		StatusCheckRollup: "[]",
	})
	defer cleanup()

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git remote add origin https://fake.origin/test/test.git

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

		// Submit first to simulate that PR exists and is up to date
		err = y.Submit(false)
		assert.NilError(t, err)

		// Set EDITOR to a script that clears the file (simulating user deleting content)
		editorScript := filepath.Join(t.TempDir(), "editor.sh")
		err = os.WriteFile(editorScript, []byte(`#!/bin/bash
# Clear the file content
echo "" > "$1"
`), 0o755)
		assert.NilError(t, err)
		t.Setenv("EDITOR", editorScript)

		// Try to merge - should fail because merge message is empty
		testutil.ExecOrFail(t, "git checkout topic-a")

		err = y.Merge(false)
		assert.ErrorContains(t, err, "merge aborted")
		assert.ErrorContains(t, err, "empty commit message")

		// Verify merge message file was cleaned up even after abort
		mergeFilePath := filepath.Join(".", ".git", "yas-merge-msg")
		_, err = os.Stat(mergeFilePath)
		assert.Assert(t, os.IsNotExist(err), "merge message file should be cleaned up after abort")
	})
}
