package test

import (
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"github.com/dansimau/yas/pkg/yascli"
	"gotest.tools/v3/assert"
)

func TestAnnotate_UpdatesPRWithStackInfo(t *testing.T) {
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
		err = y.SetParent("topic-a", "main")
		assert.NilError(t, err)
		err = y.SetParent("topic-b", "topic-a")
		assert.NilError(t, err)
		err = y.SetParent("topic-c", "topic-b")
		assert.NilError(t, err)

		// Submit topic-b to create a PR
		testutil.ExecOrFail(t, "git checkout topic-b")
		err = y.Submit()
		assert.NilError(t, err)

		// Run annotate
		assert.Equal(t, yascli.Run("annotate"), 0)
	})
}

func TestAnnotate_ErrorWhenNoPR(t *testing.T) {
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
		`)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)

		// Try to annotate without a PR - should fail
		exitCode := yascli.Run("annotate")
		assert.Assert(t, exitCode != 0, "Should fail when branch has no PR")
	})
}

