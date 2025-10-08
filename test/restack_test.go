package test

import (
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
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
