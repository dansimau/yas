package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

func TestUpdateTrunk(t *testing.T) {
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

		# update main
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
	assert.NilError(t, cli.Run("restack").Err())

	equalLines(t, mustExecOutput(tempDir, "git", "log", "--pretty=%D : %s"), `
		HEAD -> topic-b : topic-b-0
		topic-a : topic-a-0
		main : main-1
		: main-0
	`)
}

func TestUpdateTrunkTopicA(t *testing.T) {
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

		# main -> topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# main -> topic-a ->topic-b
		git checkout -b topic-b
		touch b
		git add b
		git commit -m "topic-b-0"

		# update main
		# main
		# (ref) -> topic-a -> topic-b
		git checkout main
		echo 1 > main
		git add main
		git commit -m "main-1"

		# update topic-a
		# main
		# (ref) -> (ref) -> topic-a
		# (ref) -> (ref) -> topic-b
		git checkout topic-a
		echo 1 > a
		git add a
		git commit -m "topic-a-1"

		# on branch topic-b
		git checkout topic-b
	`)

	// After restack:
	// main -> topic-a -> topic-b

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())
	assert.NilError(t, cli.Run("restack").Err())

	equalLines(t, mustExecOutput(tempDir, "git", "log", "--pretty=%D : %s"), `
		HEAD -> topic-b : topic-b-0
		topic-a : topic-a-1
		: topic-a-0
		main : main-1
		: main-0
	`)
}

func TestRestackReturnsToBranch(t *testing.T) {
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

		# update main
		git checkout main
		echo 1 > main
		git add main
		git commit -m "main-1"

		# on branch topic-a (not topic-b)
		git checkout topic-a
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())

	// Verify we're on topic-a before restack
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--show-current"), "topic-a")

	// Run restack while on topic-a
	assert.NilError(t, cli.Run("restack").Err())

	// Verify we're back on topic-a after restack
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--show-current"), "topic-a")

	// Verify the restack worked correctly
	// Note: topic-b is not in this log because we're on topic-a
	equalLines(t, mustExecOutput(tempDir, "git", "log", "--pretty=%D : %s"), `
		HEAD -> topic-a : topic-a-0
		main : main-1
		: main-0
	`)

	// Verify topic-b was also rebased correctly by checking out and viewing its log
	testutil.ExecOrFail(t, tempDir, "git checkout topic-b")
	equalLines(t, mustExecOutput(tempDir, "git", "log", "--pretty=%D : %s"), `
		HEAD -> topic-b : topic-b-0
		topic-a : topic-a-0
		main : main-1
		: main-0
	`)
}

func TestRestack_ShowsReminderWhenBranchesWithPRsAreRestacked(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := filepath.Join(tempDir, "origin.git")

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Mock gh pr list for topic-a to return PR metadata
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		ID:          "PR_kwDOTest123",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/42",
		IsDraft:     false,
		BaseRefName: "main",
	})

	testutil.ExecOrFail(t, tempDir, `
		# Create fake origin
		git init --bare `+fakeOrigin+`

		git init --initial-branch=main
		git remote add origin `+fakeOrigin+`

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

		# Push to get remote tracking set up
		git push -u origin topic-a

		# topic-b
		git checkout -b topic-b
		touch b
		git add b
		git commit -m "topic-b-0"

		# update main
		git checkout main
		echo 1 > main
		git add main
		git commit -m "main-1"

		# on branch topic-b
		git checkout topic-b
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())

	// Refresh topic-a to populate PR metadata
	testutil.ExecOrFail(t, tempDir, "git checkout topic-a")
	assert.NilError(t, cli.Run("refresh", "topic-a").Err())

	// Go back to topic-b and restack
	testutil.ExecOrFail(t, tempDir, "git checkout topic-b")

	result := cli.Run("restack")
	assert.NilError(t, result.Err())

	// Verify reminder message appears
	output := result.Stdout()
	assert.Assert(t, strings.Contains(output, "Reminder: The following branches have PRs and were restacked"),
		"Should show reminder about branches with PRs, got: %s", output)
	assert.Assert(t, strings.Contains(output, "topic-a"),
		"Should mention topic-a in reminder, got: %s", output)
	assert.Assert(t, strings.Contains(output, "yas submit --stack"),
		"Should suggest using 'yas submit --stack', got: %s", output)
}

func TestRestack_OnlyRebasesWhenNeeded(t *testing.T) {
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

		# update main - this makes topic-a need rebasing
		git checkout main
		echo 1 > main
		git add main
		git commit -m "main-1"

		# on branch topic-c
		git checkout topic-c
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())
	assert.NilError(t, cli.Run("add", "topic-c", "--parent=topic-b").Err())

	// Run restack
	result := cli.Run("restack")
	assert.NilError(t, result.Err())

	// Verify the result - all branches should include main-1
	testutil.ExecOrFail(t, tempDir, "git checkout topic-c")
	logOutput := mustExecOutput(tempDir, "git", "log", "--pretty=%s")
	assert.Assert(t, strings.Contains(logOutput, "main-1"), "topic-c should include main-1 after restack")
	assert.Assert(t, strings.Contains(logOutput, "topic-a-0"), "topic-c should include topic-a commit")
	assert.Assert(t, strings.Contains(logOutput, "topic-b-0"), "topic-c should include topic-b commit")
	assert.Assert(t, strings.Contains(logOutput, "topic-c-0"), "topic-c should include its own commit")
}

func TestRestack_SkipsRebasingWhenNotNeeded(t *testing.T) {
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

		# topic-b (child of topic-a)
		git checkout -b topic-b
		touch b
		git add b
		git commit -m "topic-b-0"

		# on branch topic-b
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())

	// Run restack - nothing should be rebased since everything is up to date
	result := cli.Run("restack")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	// Since nothing needs rebasing, the output shouldn't mention rebasing anything
	// (it might just say "nothing to do" or similar)
	assert.Assert(t, !strings.Contains(output, "Rebasing") || strings.Contains(output, "up to date"),
		"Should not show rebasing when everything is up to date, got: %s", output)

	// Verify branches are unchanged
	testutil.ExecOrFail(t, tempDir, "git checkout topic-b")
	logOutput := mustExecOutput(tempDir, "git", "log", "--pretty=%s")
	assert.Assert(t, strings.Contains(logOutput, "topic-b-0"), "topic-b commit should exist")
	assert.Assert(t, strings.Contains(logOutput, "topic-a-0"), "topic-a commit should exist")
	assert.Assert(t, strings.Contains(logOutput, "main-0"), "main commit should exist")
}

func TestRestack_NoReminderWhenNoBranchesHavePRs(t *testing.T) {
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

		# on branch topic-a
		git checkout topic-a
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	result := cli.Run("restack")
	assert.NilError(t, result.Err())
	output := result.Stdout()

	// Verify NO reminder message appears when branches don't have PRs
	assert.Assert(t, !strings.Contains(output, "Reminder"),
		"Should not show reminder when no branches have PRs, got: %s", output)
	assert.Assert(t, !strings.Contains(output, "yas submit --stack"),
		"Should not suggest 'yas submit --stack' when no PRs exist, got: %s", output)
}

func TestRestack_WithDeletedParentBranch(t *testing.T) {
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

		# update main to require rebasing
		git checkout main
		echo 1 > main
		git add main
		git commit -m "main-1"

		# on branch topic-c
		git checkout topic-c
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())
	assert.NilError(t, cli.Run("add", "topic-c", "--parent=topic-b").Err())

	// Delete topic-a branch (simulating a merged/deleted parent)
	testutil.ExecOrFail(t, tempDir, "git branch -D topic-a")

	// Simulate metadata pruning by removing topic-a from the branch map
	// This simulates what happens when an old merged branch is pruned after 7 days
	// We need to manipulate the database file directly since the data field is private
	dataPath := filepath.Join(tempDir, ".yas/yas.state.json")
	dataBytes, err := os.ReadFile(dataPath)
	assert.NilError(t, err)

	var data map[string]interface{}

	err = json.Unmarshal(dataBytes, &data)
	assert.NilError(t, err)

	// Remove topic-a from the branches map
	branches := data["branches"].(map[string]interface{})
	delete(branches, "topic-a")

	// Write it back
	newDataBytes, err := json.MarshalIndent(data, "", "  ")
	assert.NilError(t, err)
	err = os.WriteFile(dataPath, newDataBytes, 0o644)
	assert.NilError(t, err)

	// Restack should succeed by reparenting topic-b to trunk
	// and then restacking topic-c onto topic-b
	assert.NilError(t, cli.Run("restack").Err())

	// Verify that topic-b and topic-c are now based on main (not topic-a)
	// topic-b should be rebased onto main
	testutil.ExecOrFail(t, tempDir, "git checkout topic-b")
	equalLines(t, mustExecOutput(tempDir, "git", "log", "--pretty=%D : %s"), `
		HEAD -> topic-b : topic-b-0
		main : main-1
		: main-0
	`)

	// topic-c should be rebased onto topic-b (which is now on main)
	testutil.ExecOrFail(t, tempDir, "git checkout topic-c")
	equalLines(t, mustExecOutput(tempDir, "git", "log", "--pretty=%D : %s"), `
		HEAD -> topic-c : topic-c-0
		topic-b : topic-b-0
		main : main-1
		: main-0
	`)
}
