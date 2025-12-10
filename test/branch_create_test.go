package test

import (
	"strings"
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

func TestBranchCreate_WithFromFlag(t *testing.T) {
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

		git checkout main
	`)

	// Initialize yas
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("config", "set", "--no-auto-prefix-branch").Err())

	// Create a new branch from feature-a while on main
	assert.NilError(t, cli.Run("branch", "feature-b", "--from=feature-a").Err())

	// Verify we're on the new branch
	currentBranch := mustExecOutput(tempDir, "git", "branch", "--show-current")
	equalLines(t, currentBranch, "feature-b")

	// Verify the branch was created from feature-a (should have the commit from feature-a)
	logOutput := mustExecOutput(tempDir, "git", "log", "--oneline", "--all")
	assert.Assert(t, strings.Contains(logOutput, "feature-a-0"), "Expected feature-b to contain commit from feature-a")
}

func TestBranchCreate_WithFromFlagAndWorktree(t *testing.T) {
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

		git checkout main
	`)

	// Initialize yas
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("config", "set", "--no-auto-prefix-branch").Err())

	// Create a new branch from feature-a with worktree while on main
	assert.NilError(t, cli.Run("branch", "feature-b", "--from=feature-a", "--worktree").Err())

	// Verify the worktree was created
	worktreePath := tempDir + "/.yas/worktrees/feature-b"
	exitCode := mustExecExitCode(tempDir, "test", "-d", worktreePath)
	assert.Equal(t, exitCode, 0, "Expected worktree directory to exist at "+worktreePath)

	// Verify the branch exists and has the commit from feature-a
	logOutput := mustExecOutput(worktreePath, "git", "log", "--oneline")
	assert.Assert(t, strings.Contains(logOutput, "feature-a-0"), "Expected feature-b to contain commit from feature-a")
}
