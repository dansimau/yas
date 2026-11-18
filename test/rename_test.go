package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

// setupRenameRepo creates main -> testuser/feature-a -> testuser/feature-b,
// with feature-a checked out.
func setupRenameRepo(t *testing.T, tempDir string, cli *gocmdtester.CmdTester) {
	t.Helper()

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("config", "set", "--auto-prefix-branch").Err())

	assert.NilError(t, cli.Run("branch", "feature-a").Err())
	testutil.ExecOrFail(t, tempDir, `touch a && git add a && git commit -m "a-0"`)
	assert.NilError(t, cli.Run("branch", "feature-b").Err())
	testutil.ExecOrFail(t, tempDir, `touch b && git add b && git commit -m "b-0" && git checkout testuser/feature-a`)
}

func TestRename(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("GIT_AUTHOR_EMAIL", "testuser@example.com"),
	)

	setupRenameRepo(t, tempDir, cli)

	before, err := yas.NewFromRepository(tempDir)
	assert.NilError(t, err)

	oldMetadata := before.BranchMetadata("testuser/feature-a")

	result := cli.Run("rename", "foo")
	assert.NilError(t, result.Err())
	assert.Assert(t, strings.Contains(result.Stdout(), "Renamed branch 'testuser/feature-a' to 'testuser/foo'"))

	// Renamed in git, still checked out
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--show-current"), "testuser/foo")
	assert.Equal(t, mustExecExitCode(tempDir, "git", "show-ref", "--verify", "--quiet", "refs/heads/testuser/feature-a"), 1)

	// Renamed in the yas state, keeping its place in the stack
	after, err := yas.NewFromRepository(tempDir)
	assert.NilError(t, err)

	branches := after.TrackedBranches()

	_, oldExists := branches.Get("testuser/feature-a")
	assert.Assert(t, !oldExists, "old name should be gone from the state")

	renamed, exists := branches.Get("testuser/foo")
	assert.Assert(t, exists, "new name should be tracked")
	assert.Equal(t, renamed.Parent, "main")
	assert.Equal(t, renamed.BranchPoint, oldMetadata.BranchPoint)
	assert.Assert(t, renamed.Created.Equal(oldMetadata.Created))

	child, exists := branches.Get("testuser/feature-b")
	assert.Assert(t, exists)
	assert.Equal(t, child.Parent, "testuser/foo")
}

func TestRename_FullNameIsNotPrefixedTwice(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("GIT_AUTHOR_EMAIL", "testuser@example.com"),
	)

	setupRenameRepo(t, tempDir, cli)

	assert.NilError(t, cli.Run("rename", "testuser/foo").Err())
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--show-current"), "testuser/foo")
}

func TestRename_Refusals(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("GIT_AUTHOR_EMAIL", "testuser@example.com"),
	)

	setupRenameRepo(t, tempDir, cli)

	// Name already taken
	result := cli.Run("rename", "feature-b")
	assert.Assert(t, result.Err() != nil)
	assert.Assert(t, strings.Contains(result.Stderr(), "already exists"), result.Stderr())

	// Same name
	result = cli.Run("rename", "feature-a")
	assert.Assert(t, result.Err() != nil)
	assert.Assert(t, strings.Contains(result.Stderr(), "already named"), result.Stderr())

	// Invalid name
	result = cli.Run("rename", "bad..name")
	assert.Assert(t, result.Err() != nil)
	assert.Assert(t, strings.Contains(result.Stderr(), "invalid branch name"), result.Stderr())

	// Trunk
	testutil.ExecOrFail(t, tempDir, "git checkout main")

	result = cli.Run("rename", "foo")
	assert.Assert(t, result.Err() != nil)
	assert.Assert(t, strings.Contains(result.Stderr(), "trunk"), result.Stderr())

	// Nothing was renamed
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--format=%(refname:short)"), `
		main
		testuser/feature-a
		testuser/feature-b
	`)
}

func TestRename_OpenPullRequest(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("GIT_AUTHOR_EMAIL", "testuser@example.com"),
	)

	setupRenameRepo(t, tempDir, cli)

	mockGitHubPRForBranch(cli, "testuser/feature-a", yas.PullRequestMetadata{
		ID:    "PR_1",
		State: "OPEN",
		URL:   "https://github.com/test/test/pull/1",
	})
	assert.NilError(t, cli.Run("refresh", "testuser/feature-a").Err())

	result := cli.Run("rename", "foo")
	assert.Assert(t, result.Err() != nil)
	assert.Assert(t, strings.Contains(result.Stderr(), "open pull request"), result.Stderr())
	assert.Assert(t, strings.Contains(result.Stderr(), "--force"), result.Stderr())
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--show-current"), "testuser/feature-a")

	// --force renames anyway, leaving the PR behind on the old name
	result = cli.Run("rename", "--force", "foo")
	assert.NilError(t, result.Err(), result.Stderr())
	assert.Assert(t, strings.Contains(result.Stderr(), "https://github.com/test/test/pull/1 is still open"), result.Stderr())
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--show-current"), "testuser/foo")

	after, err := yas.NewFromRepository(tempDir)
	assert.NilError(t, err)
	assert.Equal(t, after.BranchMetadata("testuser/foo").GitHubPullRequest.ID, "")
}

func TestRename_PushedBranchStopsTrackingOldName(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("GIT_AUTHOR_EMAIL", "testuser@example.com"),
	)

	setupRenameRepo(t, tempDir, cli)

	testutil.ExecOrFail(t, tempDir, `
		git init --bare `+fakeOrigin+`
		git remote add origin `+fakeOrigin+`
		git push -u origin testuser/feature-a
	`)
	assert.Equal(t, upstreamOf(tempDir, "testuser/feature-a"), "origin/testuser/feature-a")

	result := cli.Run("rename", "foo")
	assert.NilError(t, result.Err())
	assert.Assert(t, strings.Contains(result.Stderr(), "no longer tracked"), result.Stderr())
	assert.Equal(t, upstreamOf(tempDir, "testuser/foo"), "")
}

func TestRename_MovesWorktreeAndShell(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	shellExecFile := filepath.Join(t.TempDir(), "shell-exec")

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("GIT_AUTHOR_EMAIL", "testuser@example.com"),
	)

	setupRenameRepo(t, tempDir, cli)

	oldWorktree := filepath.Join(tempDir, ".yas", "worktrees", "testuser", "feature-a")
	newWorktree := filepath.Join(tempDir, ".yas", "worktrees", "testuser", "foo")

	testutil.ExecOrFail(t, tempDir, `
		git checkout main
		git worktree add `+oldWorktree+` testuser/feature-a
		mkdir `+oldWorktree+`/sub
	`)

	// Run from a subdirectory of the worktree
	worktreeCLI := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(filepath.Join(oldWorktree, "sub")),
		gocmdtester.WithEnv("GIT_AUTHOR_EMAIL", "testuser@example.com"),
		gocmdtester.WithEnv("YAS_SHELL_EXEC", shellExecFile),
	)

	result := worktreeCLI.Run("rename", "foo")
	assert.NilError(t, result.Err(), result.Stderr())

	assert.Assert(t, !assertFileExists(t, oldWorktree), "old worktree path should be gone")
	equalLines(t, mustExecOutput(newWorktree, "git", "branch", "--show-current"), "testuser/foo")

	// The shell is taken to the same subdirectory of the moved worktree
	shellExec, err := os.ReadFile(shellExecFile)
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(string(shellExec), "cd "), string(shellExec))
	assert.Assert(t, strings.Contains(strings.TrimSpace(string(shellExec)), ".yas/worktrees/testuser/foo/sub"), string(shellExec))
}

func TestRename_LeavesWorktreeElsewhereAlone(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	worktree := filepath.Join(t.TempDir(), "my-worktree")

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("GIT_AUTHOR_EMAIL", "testuser@example.com"),
	)

	setupRenameRepo(t, tempDir, cli)

	testutil.ExecOrFail(t, tempDir, `
		git checkout main
		git worktree add `+worktree+` testuser/feature-a
	`)

	worktreeCLI := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(worktree),
		gocmdtester.WithEnv("GIT_AUTHOR_EMAIL", "testuser@example.com"),
	)

	assert.NilError(t, worktreeCLI.Run("rename", "foo").Err())
	equalLines(t, mustExecOutput(worktree, "git", "branch", "--show-current"), "testuser/foo")
}
