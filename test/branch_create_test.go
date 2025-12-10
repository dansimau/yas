package test

import (
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"gotest.tools/v3/assert"
)

func TestBranchCreate_WithPrefixEnabled(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("GIT_AUTHOR_EMAIL", "testuser@example.com"),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"
	`)

	// Initialize yas with auto-prefix enabled (default for new repos)
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("config", "set", "--auto-prefix-branch").Err())

	// Create a new branch using yas branch command
	assert.NilError(t, cli.Run("branch", "feature-branch").Err())

	// Verify the branch was created with the username prefix
	currentBranch := mustExecOutput(tempDir, "git", "branch", "--show-current")
	equalLines(t, currentBranch, "testuser/feature-branch")

	// Verify the branch exists
	exitCode := mustExecExitCode(tempDir, "git", "show-ref", "--verify", "--quiet", "refs/heads/testuser/feature-branch")
	assert.Equal(t, exitCode, 0, "Expected branch testuser/feature-branch to exist")
}

func TestBranchCreate_WithPrefixDisabled(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("GIT_AUTHOR_EMAIL", "testuser@example.com"),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"
	`)

	// Initialize yas with auto-prefix disabled
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("config", "set", "--no-auto-prefix-branch").Err())

	// Create a new branch using yas branch command
	assert.NilError(t, cli.Run("branch", "feature-branch").Err())

	// Verify the branch was created WITHOUT the username prefix
	currentBranch := mustExecOutput(tempDir, "git", "branch", "--show-current")
	equalLines(t, currentBranch, "feature-branch")

	// Verify the branch exists
	exitCode := mustExecExitCode(tempDir, "git", "show-ref", "--verify", "--quiet", "refs/heads/feature-branch")
	assert.Equal(t, exitCode, 0, "Expected branch feature-branch to exist")
}

func TestBranchCreate_WithParentAndPrefix(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("GIT_AUTHOR_EMAIL", "testuser@example.com"),
	)

	testutil.ExecOrFail(t, tempDir, `
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
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("config", "set", "--auto-prefix-branch").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())

	// Create a new branch with explicit parent
	assert.NilError(t, cli.Run("branch", "--parent=feature-a", "feature-b").Err())

	// Verify the branch was created with the username prefix
	currentBranch := mustExecOutput(tempDir, "git", "branch", "--show-current")
	equalLines(t, currentBranch, "testuser/feature-b")

	// Verify the branch exists
	exitCode := mustExecExitCode(tempDir, "git", "show-ref", "--verify", "--quiet", "refs/heads/testuser/feature-b")
	assert.Equal(t, exitCode, 0, "Expected branch testuser/feature-b to exist")
}

func TestBranchCreate_ExtractUsernameFromEmail(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("GIT_AUTHOR_EMAIL", "john.doe@example.com"),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"
	`)

	// Initialize yas with auto-prefix enabled
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("config", "set", "--auto-prefix-branch").Err())

	// Create a new branch
	assert.NilError(t, cli.Run("branch", "test-branch").Err())

	// Verify the username was correctly extracted from email (part before @)
	currentBranch := mustExecOutput(tempDir, "git", "branch", "--show-current")
	equalLines(t, currentBranch, "john.doe/test-branch")
}

func TestConfigSet_AutoPrefixBranch(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"
	`)

	// Initialize yas
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())

	// Set auto-prefix to true
	assert.NilError(t, cli.Run("config", "set", "--auto-prefix-branch").Err())

	// Verify config was written correctly
	testutil.ExecOrFail(t, tempDir, `
		grep -q "autoPrefixBranch: true" .yas/yas.yaml
	`)

	// Set auto-prefix to false
	assert.NilError(t, cli.Run("config", "set", "--no-auto-prefix-branch").Err())

	// Verify config was updated correctly
	testutil.ExecOrFail(t, tempDir, `
		grep -q "autoPrefixBranch: false" .yas/yas.yaml
	`)
}

func TestBranchCreate_FromParentBranch(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("GIT_AUTHOR_EMAIL", "testuser@example.com"),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"

		# Create feature-a branch with a commit
		git checkout -b feature-a
		touch a
		git add a
		git commit -m "feature-a-0"

		# Create another branch to be "current" when we create from main
		git checkout -b other-branch
		touch other
		git add other
		git commit -m "other-0"
	`)

	// Initialize yas with auto-prefix disabled
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("config", "set", "--no-auto-prefix-branch").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "other-branch", "--parent=main").Err())

	// We're currently on other-branch, but we want to create from main
	currentBranch := mustExecOutput(tempDir, "git", "branch", "--show-current")
	equalLines(t, currentBranch, "other-branch")

	// Create new branch from main without checking out main first
	assert.NilError(t, cli.Run("branch", "new-feature", "--parent=main").Err())

	// Verify we're now on new-feature
	currentBranch = mustExecOutput(tempDir, "git", "branch", "--show-current")
	equalLines(t, currentBranch, "new-feature")

	// Verify the branch was created from main (should not have commits from other-branch)
	// The log should only show main-0 commit
	output := mustExecOutput(tempDir, "git", "log", "--oneline")
	assert.Assert(t, strings.Contains(output, "main-0"), "Expected new-feature to contain main-0 commit")
	assert.Assert(t, !strings.Contains(output, "other-0"), "Expected new-feature to NOT contain other-0 commit")
	assert.Assert(t, !strings.Contains(output, "feature-a-0"), "Expected new-feature to NOT contain feature-a-0 commit")
}

func TestBranchCreate_FromParentBranchWithWorktree(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("GIT_AUTHOR_EMAIL", "testuser@example.com"),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"

		# Create feature-a branch with a commit
		git checkout -b feature-a
		touch a
		git add a
		git commit -m "feature-a-0"
		git checkout main
	`)

	// Initialize yas with auto-prefix disabled
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("config", "set", "--no-auto-prefix-branch").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())

	// We're currently on main, create from feature-a in a worktree
	currentBranch := mustExecOutput(tempDir, "git", "branch", "--show-current")
	equalLines(t, currentBranch, "main")

	// Create new branch from feature-a with worktree
	assert.NilError(t, cli.Run("branch", "new-feature", "--parent=feature-a", "--worktree").Err())

	// Verify the branch was created from feature-a
	// The log should show both main-0 and feature-a-0 commits
	output := mustExecOutput(tempDir, "git", "-C", "../new-feature", "log", "--oneline")
	assert.Assert(t, strings.Contains(output, "main-0"), "Expected new-feature to contain main-0 commit")
	assert.Assert(t, strings.Contains(output, "feature-a-0"), "Expected new-feature to contain feature-a-0 commit")
}
