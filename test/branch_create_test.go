package test

import (
	"testing"

	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yascli"
	"gotest.tools/v3/assert"
)

func TestBranchCreate_WithPrefixEnabled(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Set up git user email for the test
		t.Setenv("GIT_AUTHOR_EMAIL", "testuser@example.com")

		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"
		`)

		// Initialize yas with auto-prefix enabled (default for new repos)
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("config", "set", "--auto-prefix-branch"), 0)

		// Create a new branch using yas branch command
		assert.Equal(t, yascli.Run("branch", "feature-branch"), 0)

		// Verify the branch was created with the username prefix
		git := gitexec.WithRepo(".")
		currentBranch, err := git.GetCurrentBranchName()
		assert.NilError(t, err)
		assert.Equal(t, currentBranch, "testuser/feature-branch")

		// Verify the branch exists
		exists, err := git.BranchExists("testuser/feature-branch")
		assert.NilError(t, err)
		assert.Assert(t, exists, "Expected branch testuser/feature-branch to exist")
	})
}

func TestBranchCreate_WithPrefixDisabled(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Set up git user email for the test
		t.Setenv("GIT_AUTHOR_EMAIL", "testuser@example.com")

		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"
		`)

		// Initialize yas with auto-prefix disabled
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("config", "set", "--no-auto-prefix-branch"), 0)

		// Create a new branch using yas branch command
		assert.Equal(t, yascli.Run("branch", "feature-branch"), 0)

		// Verify the branch was created WITHOUT the username prefix
		git := gitexec.WithRepo(".")
		currentBranch, err := git.GetCurrentBranchName()
		assert.NilError(t, err)
		assert.Equal(t, currentBranch, "feature-branch")

		// Verify the branch exists
		exists, err := git.BranchExists("feature-branch")
		assert.NilError(t, err)
		assert.Assert(t, exists, "Expected branch feature-branch to exist")
	})
}

func TestBranchCreate_WithParentAndPrefix(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Set up git user email for the test
		t.Setenv("GIT_AUTHOR_EMAIL", "testuser@example.com")

		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"

			git checkout -b feature-a
			touch a
			git add a
			git commit -m "feature-a-0"
		`)

		// Initialize yas with auto-prefix enabled
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("config", "set", "--auto-prefix-branch"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=feature-a", "--parent=main"), 0)

		// Create a new branch with explicit parent
		assert.Equal(t, yascli.Run("branch", "--parent=feature-a", "feature-b"), 0)

		// Verify the branch was created with the username prefix
		git := gitexec.WithRepo(".")
		currentBranch, err := git.GetCurrentBranchName()
		assert.NilError(t, err)
		assert.Equal(t, currentBranch, "testuser/feature-b")

		// Verify the branch exists
		exists, err := git.BranchExists("testuser/feature-b")
		assert.NilError(t, err)
		assert.Assert(t, exists, "Expected branch testuser/feature-b to exist")
	})
}

func TestBranchCreate_ExtractUsernameFromEmail(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Set up git user email with different formats
		t.Setenv("GIT_AUTHOR_EMAIL", "john.doe@example.com")

		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"
		`)

		// Initialize yas with auto-prefix enabled
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("config", "set", "--auto-prefix-branch"), 0)

		// Create a new branch
		assert.Equal(t, yascli.Run("branch", "test-branch"), 0)

		// Verify the username was correctly extracted from email (part before @)
		git := gitexec.WithRepo(".")
		currentBranch, err := git.GetCurrentBranchName()
		assert.NilError(t, err)
		assert.Equal(t, currentBranch, "john.doe/test-branch")
	})
}

func TestConfigSet_AutoPrefixBranch(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"
		`)

		// Initialize yas
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)

		// Set auto-prefix to true
		assert.Equal(t, yascli.Run("config", "set", "--auto-prefix-branch"), 0)

		// Verify config was written correctly
		testutil.ExecOrFail(t, `
			grep -q "autoPrefixBranch: true" .git/yas.yaml
		`)

		// Set auto-prefix to false
		assert.Equal(t, yascli.Run("config", "set", "--no-auto-prefix-branch"), 0)

		// Verify config was updated correctly
		testutil.ExecOrFail(t, `
			grep -q "autoPrefixBranch: false" .git/yas.yaml
		`)
	})
}
