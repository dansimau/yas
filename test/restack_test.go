package test

import (
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"github.com/dansimau/yas/pkg/yascli"
	"gotest.tools/v3/assert"
)

func TestUpdateTrunk(t *testing.T) {
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

			# update main
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
		assert.Equal(t, yascli.Run("restack"), 0)

		equalLines(t, mustExecOutput("git", "log", "--pretty=%D : %s"), `
			HEAD -> topic-b : topic-b-0
			topic-a : topic-a-0
			main : main-1
			: main-0
		`)
	})
}

func TestUpdateTrunkTopicA(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main

			# main
			touch main
			git add main
			git commit -m "main-0"

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

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-b", "--parent=topic-a"), 0)
		assert.Equal(t, yascli.Run("restack"), 0)

		equalLines(t, mustExecOutput("git", "log", "--pretty=%D : %s"), `
			HEAD -> topic-b : topic-b-0
			topic-a : topic-a-1
			: topic-a-0
			main : main-1
			: main-0
		`)
	})
}

func TestRestackReturnsToBranch(t *testing.T) {
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

			# update main
			git checkout main
			echo 1 > main
			git add main
			git commit -m "main-1"

			# on branch topic-a (not topic-b)
			git checkout topic-a
		`)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-b", "--parent=topic-a"), 0)

		// Verify we're on topic-a before restack
		equalLines(t, mustExecOutput("git", "branch", "--show-current"), "topic-a")

		// Run restack while on topic-a
		assert.Equal(t, yascli.Run("restack"), 0)

		// Verify we're back on topic-a after restack
		equalLines(t, mustExecOutput("git", "branch", "--show-current"), "topic-a")

		// Verify the restack worked correctly
		// Note: topic-b is not in this log because we're on topic-a
		equalLines(t, mustExecOutput("git", "log", "--pretty=%D : %s"), `
			HEAD -> topic-a : topic-a-0
			main : main-1
			: main-0
		`)

		// Verify topic-b was also rebased correctly by checking out and viewing its log
		testutil.ExecOrFail(t, "git checkout topic-b")
		equalLines(t, mustExecOutput("git", "log", "--pretty=%D : %s"), `
			HEAD -> topic-b : topic-b-0
			topic-a : topic-a-0
			main : main-1
			: main-0
		`)
	})
}

func TestRestack_ShowsReminderWhenBranchesWithPRsAreRestacked(t *testing.T) {
	_, cleanup := setupMockCommandsWithPR(t, mockPROptions{
		ID:    "PR_kwDOTest123",
		State: "OPEN",
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

		// Submit topic-a to create a PR and populate metadata
		testutil.ExecOrFail(t, "git checkout topic-a")

		err = y.Submit()
		assert.NilError(t, err)

		// Go back to topic-b and restack
		testutil.ExecOrFail(t, "git checkout topic-b")
		output := captureStdout(t, func() {
			assert.Equal(t, yascli.Run("restack"), 0)
		})

		// Verify reminder message appears
		assert.Assert(t, strings.Contains(output, "Reminder: The following branches have PRs and were restacked"),
			"Should show reminder about branches with PRs, got: %s", output)
		assert.Assert(t, strings.Contains(output, "topic-a"),
			"Should mention topic-a in reminder, got: %s", output)
		assert.Assert(t, strings.Contains(output, "yas submit --stack"),
			"Should suggest using 'yas submit --stack', got: %s", output)
	})
}

func TestRestack_OnlyRebasesWhenNeeded(t *testing.T) {
	cmdLogFile, cleanup := setupMockCommands(t, "")
	defer cleanup()

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

		// Run restack
		err = y.Restack()
		assert.NilError(t, err)

		// Parse the command log
		commands, err := parseCmdLog(cmdLogFile)
		assert.NilError(t, err)

		// Count how many rebase commands were called
		rebaseCount := 0

		for _, cmd := range commands {
			// git -c core.hooksPath=/dev/null rebase ...
			if len(cmd) >= 5 && cmd[0] == "git" && cmd[3] == "rebase" {
				rebaseCount++
			}
		}

		// topic-a needs rebasing (main changed)
		// topic-b needs rebasing (because topic-a was rebased)
		// topic-c needs rebasing (because topic-b was rebased)
		assert.Equal(t, rebaseCount, 3, "Should have called git rebase 3 times (for topic-a, topic-b, topic-c)")

		// Verify specific rebase commands (using new --onto format)
		assert.Assert(t, wasCalled(commands, "git", "-c", "core.hooksPath=/dev/null", "rebase", "--onto", "main"),
			"Should rebase topic-a onto main")
		assert.Assert(t, wasCalled(commands, "git", "-c", "core.hooksPath=/dev/null", "rebase", "--onto", "topic-a"),
			"Should rebase topic-b onto topic-a")
		assert.Assert(t, wasCalled(commands, "git", "-c", "core.hooksPath=/dev/null", "rebase", "--onto", "topic-b"),
			"Should rebase topic-c onto topic-b")
	})
}

func TestRestack_SkipsRebasingWhenNotNeeded(t *testing.T) {
	cmdLogFile, cleanup := setupMockCommands(t, "")
	defer cleanup()

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

			# topic-b (child of topic-a)
			git checkout -b topic-b
			touch b
			git add b
			git commit -m "topic-b-0"

			# on branch topic-b
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

		// Run restack - nothing should be rebased since everything is up to date
		err = y.Restack()
		assert.NilError(t, err)

		// Parse the command log
		commands, err := parseCmdLog(cmdLogFile)
		assert.NilError(t, err)

		// Count how many rebase commands were called
		rebaseCount := 0

		for _, cmd := range commands {
			// git -c core.hooksPath=/dev/null rebase ...
			if len(cmd) >= 5 && cmd[0] == "git" && cmd[3] == "rebase" {
				rebaseCount++
			}
		}

		// Nothing needs rebasing, so no rebase commands should have been called
		assert.Equal(t, rebaseCount, 0, "Should not have called git rebase (everything is up to date)")
	})
}

func TestRestack_NoReminderWhenNoBranchesHavePRs(t *testing.T) {
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

			# on branch topic-a
			git checkout topic-a
		`)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)

		output := captureStdout(t, func() {
			assert.Equal(t, yascli.Run("restack"), 0)
		})

		// Verify NO reminder message appears when branches don't have PRs
		assert.Assert(t, !strings.Contains(output, "Reminder"),
			"Should not show reminder when no branches have PRs, got: %s", output)
		assert.Assert(t, !strings.Contains(output, "yas submit --stack"),
			"Should not suggest 'yas submit --stack' when no PRs exist, got: %s", output)
	})
}
