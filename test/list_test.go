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
