package test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"gotest.tools/v3/assert"
)

func TestMissingGitRepo(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Run list in a directory without a git repo
	result := cli.Run("list")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("hint: specify --repo"), "Expected hint about --repo in stderr, got: %s", result.Stderr())
}

func TestNotInitialized(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `git init`)

	result := cli.Run("list")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("repository not configured"), "Expected 'repository not configured' in stderr, got: %s", result.Stderr())
}

func TestNotInitialized_Worktree(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		# Create main repo
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"

		# Create a worktree
		git worktree add worktree-dir
	`)

	// Run from within the worktree (where .git is a file, not a directory)
	result := cli.Run("--repo=worktree-dir", "list")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("repository not configured"),
		"Expected 'repository not configured' error in worktree, got: %s", result.Stderr())
}

func TestInit_FromInsideWorktree(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	worktreePath := filepath.Join(tempDir, "worktrees", "feature-a")

	// Create main repo with a worktree
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
		mkdir -p `+filepath.Dir(worktreePath)+`
		git worktree add `+worktreePath+` feature-a
	`)

	// Run 'yas init' from inside the worktree
	cliInWorktree := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(worktreePath),
		gocmdtester.WithStdin(strings.NewReader("main\n")), // Answer the trunk branch prompt
	)

	result := cliInWorktree.Run("init")
	assert.NilError(t, result.Err(), "yas init should succeed from worktree, got: %s", result.Stderr())

	// Verify config was written to the primary repo, not the worktree
	output := result.Stdout()
	assert.Assert(t, strings.Contains(output, tempDir) && !strings.Contains(output, "worktrees/feature-a"),
		"Config should be saved to primary repo path, not worktree. Got: %s", output)

	// Verify that 'yas list' works from both the worktree and the main repo
	cliInMain := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	resultFromMain := cliInMain.Run("list")
	assert.NilError(t, resultFromMain.Err(), "yas list should work from main repo after init from worktree")

	resultFromWorktree := cliInWorktree.Run("list")
	assert.NilError(t, resultFromWorktree.Err(), "yas list should work from worktree after init from worktree")
}

func TestConfig_WorktreeBranch(t *testing.T) {
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

	// Enable worktree-branch config
	result := cli.Run("config", "set", "--worktree-branch")
	assert.NilError(t, result.Err())
	assert.Assert(t, strings.Contains(result.Stdout(), "Wrote config to:"))

	// Create a new branch - should automatically create a worktree
	tempFile := filepath.Join(t.TempDir(), "shell-exec")
	cliWithEnv := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("YAS_SHELL_EXEC", tempFile),
	)

	result = cliWithEnv.Run("branch", "feature-a")
	assert.NilError(t, result.Err())

	// Verify that a worktree was created
	worktreeList := mustExecOutput(tempDir, "git", "worktree", "list")
	assert.Assert(t, strings.Contains(worktreeList, "worktrees/feature-a"), "Expected worktree to be created")

	// Disable worktree-branch config
	result = cli.Run("config", "set", "--no-worktree-branch")
	assert.NilError(t, result.Err())

	// Create another branch - should NOT create a worktree
	result = cli.Run("branch", "feature-b")
	assert.NilError(t, result.Err())

	// Verify that no new worktree was created for feature-b
	worktreeList = mustExecOutput(tempDir, "git", "worktree", "list")
	assert.Assert(t, !strings.Contains(worktreeList, "worktrees/feature-b"), "Expected no worktree for feature-b")
}

func TestConfigSet_RejectsPositionalArguments(t *testing.T) {
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

	// Try to set config with a positional argument (typo: missing --)
	result := cli.Run("config", "set", "worktree-branch")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("unknown argument: worktree-branch"),
		"Expected 'unknown argument' error, got: %s", result.Stderr())
}

func TestConfig_TrunkBranchAliases(t *testing.T) {
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

		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())

	// Without aliases configured, "master" is not a known branch
	result := cli.Run("add", "topic-a", "--parent=master")
	assert.Equal(t, result.ExitCode(), 1, "expected failure without aliases, got stdout: %s stderr: %s", result.Stdout(), result.Stderr())

	// Configure aliases
	result = cli.Run("config", "set", "--trunk-branch-aliases=master,trunk")
	assert.NilError(t, result.Err(), result.Stderr())
	assert.Assert(t, strings.Contains(result.Stdout(), "Wrote config to:"))

	configYAML := mustExecOutput(tempDir, "cat", ".yas/yas.yaml")
	assert.Assert(t, strings.Contains(configYAML, "trunkBranchAliases:"), "expected aliases in config, got: %s", configYAML)
	assert.Assert(t, strings.Contains(configYAML, "- master"), "expected master alias in config, got: %s", configYAML)
	assert.Assert(t, strings.Contains(configYAML, "- trunk"), "expected trunk alias in config, got: %s", configYAML)

	// The alias is now replaced with the real trunk branch name, and an
	// informational (cyan) message is printed to stderr
	result = cli.Run("add", "topic-a", "--parent=master")
	assert.NilError(t, result.Err(), result.Stderr())

	aliasNotice := "\x1b[36mReplaced branch name from 'master' to 'main' (trunk branch alias)\x1b[0m"
	assert.Assert(t, strings.Contains(result.Stderr(), aliasNotice), "expected alias notice in stderr, got: %s", result.Stderr())

	// topic-a's parent is main, not master
	equalLines(t, cli.Run("list").Stdout(), `
		main
		└── topic-a (not submitted) *
	`)

	// Aliases work for positional branch arguments too, and the message is
	// only printed once per invocation even when several arguments are replaced
	result = cli.Run("state", "show", "master", "trunk")
	assert.NilError(t, result.Err(), result.Stderr())
	assert.Equal(t, strings.Count(result.Stderr(), "Replaced branch name from"), 1, "expected exactly one alias notice, got: %s", result.Stderr())
	assert.Assert(t, strings.Contains(result.Stderr(), "from 'master' to 'main'"), "expected alias notice for master, got: %s", result.Stderr())

	// The real trunk name is never reported as replaced
	result = cli.Run("restack", "main", "--dry-run")
	assert.NilError(t, result.Err(), result.Stderr())
	assert.Assert(t, !strings.Contains(result.Stderr(), "Replaced branch name"), "unexpected alias notice, got: %s", result.Stderr())

	// Switching to an alias switches to the trunk branch
	result = cli.Run("branch", "master")
	assert.NilError(t, result.Err(), result.Stderr())
	assert.Assert(t, strings.Contains(result.Stderr(), aliasNotice), "expected alias notice in stderr, got: %s", result.Stderr())
	assert.Equal(t, strings.TrimSpace(mustExecOutput(tempDir, "git", "rev-parse", "--abbrev-ref", "HEAD")), "main")

	// Clearing the aliases disables the replacement
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch-aliases=").Err())

	configYAML = mustExecOutput(tempDir, "cat", ".yas/yas.yaml")
	assert.Assert(t, !strings.Contains(configYAML, "trunkBranchAliases"), "expected aliases removed from config, got: %s", configYAML)

	result = cli.Run("add", "topic-a", "--parent=master")
	assert.Equal(t, result.ExitCode(), 1, "expected failure after clearing aliases, got stdout: %s stderr: %s", result.Stdout(), result.Stderr())
}
