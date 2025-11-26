package test

import (
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"gotest.tools/v3/assert"
)

func TestRestack_RefusesWhenRestackInProgress(t *testing.T) {
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

		# topic-a: modify file
		git checkout -b topic-a
		echo "line2-from-a" >> file.txt
		git add file.txt
		git commit -m "topic-a-0"

		# update main: modify the same file differently
		git checkout main
		echo "line2-from-main" >> file.txt
		git add file.txt
		git commit -m "main-1"

		# on branch topic-a
		git checkout topic-a
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Run restack - it should fail due to conflict
	result := cli.Run("restack")
	assert.Equal(t, result.ExitCode(), 1, "restack should fail due to conflict")

	// Verify that restack state was saved
	assert.Assert(t, assertRestackStateExists(t, tempDir), "restack state should be saved")

	// Try to run restack again - should be refused
	result = cli.Run("restack")
	assert.Equal(t, result.ExitCode(), 1, "restack should be refused when already in progress")

	// Clean up
	assert.NilError(t, cli.Run("abort").Err())
}

func TestSubmit_RefusesWhenRestackInProgress(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		git remote add origin https://fake.origin/test/test.git

		# main
		echo "line1" > file.txt
		git add file.txt
		git commit -m "main-0"

		# topic-a: modify file
		git checkout -b topic-a
		echo "line2-from-a" >> file.txt
		git add file.txt
		git commit -m "topic-a-0"

		# update main: modify the same file differently
		git checkout main
		echo "line2-from-main" >> file.txt
		git add file.txt
		git commit -m "main-1"

		# on branch topic-a
		git checkout topic-a
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Run restack - it should fail due to conflict
	result := cli.Run("restack")
	assert.Equal(t, result.ExitCode(), 1, "restack should fail due to conflict")

	// Verify that restack state was saved
	assert.Assert(t, assertRestackStateExists(t, tempDir), "restack state should be saved")

	// Try to run submit - should be refused
	result = cli.Run("submit")
	assert.Equal(t, result.ExitCode(), 1, "submit should be refused when restack in progress")

	// Clean up
	assert.NilError(t, cli.Run("abort").Err())
}

func TestSync_RefusesWhenRestackInProgress(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		git remote add origin https://fake.origin/test/test.git

		# main
		echo "line1" > file.txt
		git add file.txt
		git commit -m "main-0"

		# topic-a: modify file
		git checkout -b topic-a
		echo "line2-from-a" >> file.txt
		git add file.txt
		git commit -m "topic-a-0"

		# update main: modify the same file differently
		git checkout main
		echo "line2-from-main" >> file.txt
		git add file.txt
		git commit -m "main-1"

		# on branch topic-a
		git checkout topic-a
	`)

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Run restack - it should fail due to conflict
	result := cli.Run("restack")
	assert.Equal(t, result.ExitCode(), 1, "restack should fail due to conflict")

	// Verify that restack state was saved
	assert.Assert(t, assertRestackStateExists(t, tempDir), "restack state should be saved")

	// Try to run sync - should be refused
	result = cli.Run("sync")
	assert.Equal(t, result.ExitCode(), 1, "sync should be refused when restack in progress")

	// Clean up
	assert.NilError(t, cli.Run("abort").Err())
}
