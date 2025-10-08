package test

import (
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yascli"
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
		output := captureStdout(func() {
			assert.Equal(t, yascli.Run("list"), 0)
		})

		// topic-a should show "(needs restack)" because main has new commits
		assert.Assert(t, strings.Contains(output, "topic-a") && strings.Contains(output, "(needs restack)"),
			"topic-a should show '(needs restack)' but got: %s", output)

		// topic-b should NOT show "(needs restack)" because topic-a hasn't changed
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if strings.Contains(line, "topic-b") {
				assert.Assert(t, !strings.Contains(line, "(needs restack)"),
					"topic-b should not show '(needs restack)' but got: %s", line)
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
		output := captureStdout(func() {
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
		output := captureStdout(func() {
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
		output := captureStdout(func() {
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
		fullOutput := captureStdout(func() {
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
		stackOutput := captureStdout(func() {
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
