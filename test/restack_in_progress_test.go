package test

import (
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"github.com/dansimau/yas/pkg/yascli"
	"gotest.tools/v3/assert"
)

func TestRestack_RefusesWhenRestackInProgress(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
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
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "topic-a", "--parent=main"), 0)

		// Run restack - it should fail due to conflict
		exitCode := yascli.Run("restack")
		assert.Equal(t, exitCode, 1, "restack should fail due to conflict")

		// Verify that restack state was saved
		assert.Assert(t, yas.RestackStateExists("."), "restack state should be saved")

		// Try to run restack again - should be refused
		exitCode = yascli.Run("restack")
		assert.Equal(t, exitCode, 1, "restack should be refused when already in progress")

		// Clean up
		assert.Equal(t, yascli.Run("abort"), 0)
	})
}

func TestSubmit_RefusesWhenRestackInProgress(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
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
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "topic-a", "--parent=main"), 0)

		// Run restack - it should fail due to conflict
		exitCode := yascli.Run("restack")
		assert.Equal(t, exitCode, 1, "restack should fail due to conflict")

		// Verify that restack state was saved
		assert.Assert(t, yas.RestackStateExists("."), "restack state should be saved")

		// Try to run submit - should be refused
		exitCode = yascli.Run("submit")
		assert.Equal(t, exitCode, 1, "submit should be refused when restack in progress")

		// Clean up
		assert.Equal(t, yascli.Run("abort"), 0)
	})
}

func TestSync_RefusesWhenRestackInProgress(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
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
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "topic-a", "--parent=main"), 0)

		// Run restack - it should fail due to conflict
		exitCode := yascli.Run("restack")
		assert.Equal(t, exitCode, 1, "restack should fail due to conflict")

		// Verify that restack state was saved
		assert.Assert(t, yas.RestackStateExists("."), "restack state should be saved")

		// Try to run sync - should be refused
		exitCode = yascli.Run("sync")
		assert.Equal(t, exitCode, 1, "sync should be refused when restack in progress")

		// Clean up
		assert.Equal(t, yascli.Run("abort"), 0)
	})
}
