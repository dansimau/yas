package test

import (
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"github.com/dansimau/yas/pkg/yascli"
	"gotest.tools/v3/assert"
)

// TestRestack_FatalErrorDoesNotSaveState tests that when a rebase fails with
// a fatal error (e.g., unstashed changes), we don't save restack state.
func TestRestack_FatalErrorDoesNotSaveState(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main

			# main
			echo "line1" > file.txt
			git add file.txt
			git commit -m "main-0"

			# topic-a
			git checkout -b topic-a
			echo "line2" >> file.txt
			git add file.txt
			git commit -m "topic-a-0"

			# update main
			git checkout main
			echo "updated" > main.txt
			git add main.txt
			git commit -m "main-1"

			# on branch topic-a
			git checkout topic-a

			# Create unstashed changes that will cause rebase to fail
			echo "uncommitted change" >> file.txt
		`)

		// Initialize yas config
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "topic-a", "--parent=main"), 0)

		// Run restack - it should fail due to unstashed changes
		exitCode := yascli.Run("restack")
		assert.Equal(t, exitCode, 1, "restack should fail due to unstashed changes")

		// Verify that restack state was NOT saved (fatal error, not conflict)
		assert.Assert(t, !yas.RestackStateExists("."), "restack state should NOT be saved for fatal errors")

		// Verify we're still on topic-a
		equalLines(t, mustExecOutput("git", "branch", "--show-current"), "topic-a")

		// Verify no rebase is in progress
		testutil.ExecOrFail(t, "test ! -d .git/rebase-merge")
		testutil.ExecOrFail(t, "test ! -d .git/rebase-apply")

		// Verify topic-a still has old commits (not rebased)
		logOutput := mustExecOutput("git", "log", "--pretty=%s")
		assert.Assert(t, strings.Contains(logOutput, "topic-a-0"), "topic-a commit should exist")
		assert.Assert(t, strings.Contains(logOutput, "main-0"), "main-0 commit should exist")
		assert.Assert(t, !strings.Contains(logOutput, "main-1"), "main-1 should NOT be in history")
	})
}
