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

func TestBranchCreate_WithDifferentParentBranchesFromParent(t *testing.T) {
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

	// Initialize yas with auto-prefix disabled to keep branch names simple.
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("config", "set", "--no-auto-prefix-branch").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())

	// We are currently on feature-a (which contains the "a" file). Stage a new
	// file that should NOT be carried over to the new branch.
	testutil.ExecOrFail(t, tempDir, `
		touch staged-file
		git add staged-file
	`)

	// Create a new branch with an explicit parent that differs from the
	// current branch. The branch should be created fresh from main.
	assert.NilError(t, cli.Run("branch", "--parent=main", "feature-b").Err())

	// Verify we switched to the new branch.
	currentBranch := mustExecOutput(tempDir, "git", "branch", "--show-current")
	equalLines(t, currentBranch, "feature-b")

	// The new branch should be based on main, so it must NOT contain the "a"
	// file that exists only on feature-a.
	exitCode := mustExecExitCode(tempDir, "git", "cat-file", "-e", "HEAD:a")
	assert.Assert(t, exitCode != 0, "Expected file 'a' (from feature-a) to NOT exist on feature-b")

	// The staged file should not have been committed to the new branch.
	exitCode = mustExecExitCode(tempDir, "git", "cat-file", "-e", "HEAD:staged-file")
	assert.Assert(t, exitCode != 0, "Expected staged file to NOT be committed onto feature-b")

	// HEAD of feature-b should match HEAD of main.
	mainRef := strings.TrimSpace(mustExecOutput(tempDir, "git", "rev-parse", "main"))
	headRef := strings.TrimSpace(mustExecOutput(tempDir, "git", "rev-parse", "HEAD"))
	assert.Equal(t, headRef, mainRef, "Expected feature-b to be based on main's HEAD")
}

func TestBranchCreate_FromRemoteOnlyParent(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("GIT_AUTHOR_EMAIL", "testuser@example.com"),
	)

	testutil.ExecOrFail(t, tempDir, `
		# Set up "remote" bare repository
		git init --bare `+fakeOrigin+`

		git init --initial-branch=main
		git remote add origin `+fakeOrigin+`
		touch main
		git add main
		git commit -m "main-0"

		# Create a parent branch, push it, then delete it locally so it only
		# exists as origin/base.
		git checkout -b base
		touch base-file
		git add base-file
		git commit -m "base-0"
		git push origin base
		git checkout main
		git branch -D base
		git fetch origin
	`)

	// Initialize yas before any detach/remote-only state.
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("config", "set", "--no-auto-prefix-branch").Err())

	// Sanity: the parent does not exist locally, only as origin/base.
	localBase := mustExecOutput(tempDir, "git", "branch", "--list", "base")
	equalLines(t, localBase, "")

	// Create a new branch from the remote-only parent.
	assert.NilError(t, cli.Run("branch", "--parent=base", "feature").Err())

	// Verify we switched to the new branch.
	currentBranch := mustExecOutput(tempDir, "git", "branch", "--show-current")
	equalLines(t, currentBranch, "feature")

	// The new branch should be based on origin/base's HEAD.
	originBaseRef := strings.TrimSpace(mustExecOutput(tempDir, "git", "rev-parse", "origin/base"))
	headRef := strings.TrimSpace(mustExecOutput(tempDir, "git", "rev-parse", "HEAD"))
	assert.Equal(t, headRef, originBaseRef, "Expected feature to be based on origin/base")
}

func TestBranchCreate_DetachedHeadWithExplicitParent(t *testing.T) {
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
		touch main2
		git add main2
		git commit -m "main-1"
	`)

	// Initialize yas while still on a branch.
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("config", "set", "--no-auto-prefix-branch").Err())

	// Detach HEAD.
	testutil.ExecOrFail(t, tempDir, `git checkout --detach HEAD~1`)

	// Sanity: we are in detached HEAD (no current branch).
	currentBranch := mustExecOutput(tempDir, "git", "branch", "--show-current")
	equalLines(t, currentBranch, "")

	// Creating a branch with an explicit parent should work even in detached
	// HEAD, since the current branch name is not required.
	assert.NilError(t, cli.Run("branch", "--parent=main", "feature").Err())

	// Verify we are now on the new branch.
	currentBranch = mustExecOutput(tempDir, "git", "branch", "--show-current")
	equalLines(t, currentBranch, "feature")

	// The new branch should be based on main's HEAD.
	mainRef := strings.TrimSpace(mustExecOutput(tempDir, "git", "rev-parse", "main"))
	headRef := strings.TrimSpace(mustExecOutput(tempDir, "git", "rev-parse", "HEAD"))
	assert.Equal(t, headRef, mainRef, "Expected feature to be based on main's HEAD")
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
