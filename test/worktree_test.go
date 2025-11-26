package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestWorktree_SwitchToExistingWorktree(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create main repo with two branches and worktree
	worktreePath := filepath.Join(tempDir, "worktrees", "feature-a")
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
		git worktree add `+worktreePath+` feature-a
	`)

	// Initialize yas
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())

	// Try to switch to feature-a with YAS_SHELL_EXEC set
	tempFile := filepath.Join(t.TempDir(), "shell-exec")

	cliWithEnv := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("YAS_SHELL_EXEC", tempFile),
	)

	assert.NilError(t, cliWithEnv.Run("branch", "feature-a").Err())

	// Verify the temp file contains the cd command
	content, err := os.ReadFile(tempFile)
	assert.NilError(t, err)

	contentStr := string(content)
	assert.Assert(t, cmp.Contains(contentStr, "cd "))
	assert.Assert(t, cmp.Contains(contentStr, "worktrees/feature-a"))
	assert.Assert(t, cmp.Contains(contentStr, "echo "))
	assert.Assert(t, cmp.Contains(contentStr, "Switched to branch"))
}

func TestWorktree_FallbackToCheckoutWhenNoWorktree(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create main repo with two branches but no worktrees
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
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())

	// Try to switch to feature-a (should fall back to normal checkout)
	tempFile := filepath.Join(t.TempDir(), "shell-exec")

	cliWithEnv := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("YAS_SHELL_EXEC", tempFile),
	)

	assert.NilError(t, cliWithEnv.Run("branch", "feature-a").Err())

	// Verify we're on feature-a branch now (normal checkout happened)
	output := mustExecOutput(tempDir, "git", "branch", "--show-current")
	assert.Equal(t, strings.TrimSpace(output), "feature-a")

	// Verify the temp file is empty or doesn't exist (no cd command was written)
	content, err := os.ReadFile(tempFile)
	if err == nil {
		assert.Equal(t, string(content), "")
	}
	// If file doesn't exist, that's also fine - it means nothing was written
}

func TestWorktree_ErrorWhenHookNotInstalled(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create main repo with worktree
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

		# Add worktree
		git worktree add worktrees feature-a
	`)

	// Initialize yas
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())

	// Try to switch to feature-a WITHOUT YAS_SHELL_EXEC set
	result := cli.Run("branch", "feature-a")
	assert.Equal(t, result.ExitCode(), 1)

	stderr := result.Stderr()
	assert.Assert(t, cmp.Contains(stderr, "YAS_SHELL_EXEC environment variable not set"))
	assert.Assert(t, cmp.Contains(stderr, "install the yas shell hook"))
	assert.Assert(t, cmp.Contains(stderr, "yas hook bash"))
	assert.Assert(t, cmp.Contains(stderr, "yas hook zsh"))
}

func TestHook_BashOutputsHookCode(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	result := cli.Run("hook", "bash")
	assert.NilError(t, result.Err())
	assert.Equal(t, result.Stderr(), "")

	stdout := result.Stdout()
	// Verify the hook code contains expected elements
	assert.Assert(t, cmp.Contains(stdout, "yas() {"))
	assert.Assert(t, cmp.Contains(stdout, "YAS_SHELL_EXEC"))
	assert.Assert(t, cmp.Contains(stdout, "mktemp"))
	assert.Assert(t, cmp.Contains(stdout, "command yas"))
	assert.Assert(t, cmp.Contains(stdout, "source"))
	assert.Assert(t, cmp.Contains(stdout, "rm -f"))
}

func TestHook_ZshOutputsHookCode(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	result := cli.Run("hook", "zsh")
	assert.NilError(t, result.Err())
	assert.Equal(t, result.Stderr(), "")

	stdout := result.Stdout()
	// Verify the hook code contains expected elements
	assert.Assert(t, cmp.Contains(stdout, "yas() {"))
	assert.Assert(t, cmp.Contains(stdout, "YAS_SHELL_EXEC"))
	assert.Assert(t, cmp.Contains(stdout, "mktemp"))
	assert.Assert(t, cmp.Contains(stdout, "command yas"))
	assert.Assert(t, cmp.Contains(stdout, "source"))
	assert.Assert(t, cmp.Contains(stdout, "rm -f"))
}

func TestWorktree_SwitchFromWorktreeToMainBranch(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create main repo with a branch that has a worktree
	worktreePath := filepath.Join(tempDir, "worktrees", "feature-a")
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
		git worktree add `+worktreePath+` feature-a
	`)

	// Initialize yas
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())

	// Now try to switch to main from within the worktree
	tempFile := filepath.Join(t.TempDir(), "shell-exec")

	cliInWorktree := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(worktreePath),
		gocmdtester.WithEnv("YAS_SHELL_EXEC", tempFile),
	)

	// Run yas branch from worktree directory
	assert.NilError(t, cliInWorktree.Run("branch", "main").Err())

	// Verify the temp file contains cd to primary repo
	content, err := os.ReadFile(tempFile)
	assert.NilError(t, err)

	contentStr := string(content)
	assert.Assert(t, cmp.Contains(contentStr, "cd "))
	assert.Assert(t, cmp.Contains(contentStr, tempDir))
	assert.Assert(t, cmp.Contains(contentStr, "Switched to branch"))
	assert.Assert(t, cmp.Contains(contentStr, "main"))
}

func TestWorktree_SwitchFromWorktreeToAnotherNonWorktreeBranch(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create main repo with multiple branches and worktree for feature-a
	worktreePath := filepath.Join(tempDir, "worktrees", "feature-a")
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
		git checkout -b feature-b
		touch b
		git add b
		git commit -m "feature-b-0"

		git checkout main
		git worktree add `+worktreePath+` feature-a
	`)

	// Initialize yas
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "feature-b", "--parent=main").Err())

	// Now try to switch to feature-b from within feature-a worktree
	tempFile := filepath.Join(t.TempDir(), "shell-exec")

	cliInWorktree := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(worktreePath),
		gocmdtester.WithEnv("YAS_SHELL_EXEC", tempFile),
	)

	// Run yas branch from worktree directory
	assert.NilError(t, cliInWorktree.Run("branch", "feature-b").Err())

	// Verify the temp file contains cd to primary repo and yas br command
	content, err := os.ReadFile(tempFile)
	assert.NilError(t, err)

	contentStr := string(content)
	assert.Assert(t, cmp.Contains(contentStr, "cd "))
	assert.Assert(t, cmp.Contains(contentStr, tempDir))
	assert.Assert(t, cmp.Contains(contentStr, "yas"))
	assert.Assert(t, cmp.Contains(contentStr, "br"))
	assert.Assert(t, cmp.Contains(contentStr, "feature-b"))
}
