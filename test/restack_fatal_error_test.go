package test

import (
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"gotest.tools/v3/assert"
)

// TestRestack_FatalErrorDoesNotSaveState tests that when a rebase fails with
// a fatal error (e.g., unstashed changes), we don't save restack state.
func TestRestack_FatalErrorDoesNotSaveState(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
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
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Run restack - it should fail due to unstashed changes
	result := cli.Run("restack")
	assert.Equal(t, result.ExitCode(), 1, "restack should fail due to unstashed changes")

	// Verify that restack state was NOT saved (fatal error, not conflict)
	assert.Assert(t, !assertRestackStateExists(t, tempDir), "restack state should NOT be saved for fatal errors")

	// Verify we're still on topic-a
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--show-current"), "topic-a")

	// Verify no rebase is in progress
	testutil.ExecOrFail(t, tempDir, "test ! -d .git/rebase-merge")
	testutil.ExecOrFail(t, tempDir, "test ! -d .git/rebase-apply")

	// Verify topic-a still has old commits (not rebased)
	logOutput := mustExecOutput(tempDir, "git", "log", "--pretty=%s")
	assert.Assert(t, strings.Contains(logOutput, "topic-a-0"), "topic-a commit should exist")
	assert.Assert(t, strings.Contains(logOutput, "main-0"), "main-0 commit should exist")
	assert.Assert(t, !strings.Contains(logOutput, "main-1"), "main-1 should NOT be in history")
}
