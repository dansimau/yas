package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yascli"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestWorktree_SwitchToExistingWorktree(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Create main repo with two branches
		testutil.ExecOrFail(t, `
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
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "feature-a", "--parent=main"), 0)

		// Create a worktree for feature-a
		worktreePath := filepath.Join(".", "worktrees", "feature-a")
		testutil.ExecOrFail(t, "git worktree add "+worktreePath+" feature-a")

		// Ensure we're back on main in the primary repo
		testutil.ExecOrFail(t, "git checkout main")

		// Try to switch to feature-a with YAS_SHELL_EXEC set
		tempFile := filepath.Join(t.TempDir(), "shell-exec")

		t.Setenv("YAS_SHELL_EXEC", tempFile)

		assert.Equal(t, yascli.Run("branch", "feature-a"), 0)

		// Verify the temp file contains the cd command
		content, err := os.ReadFile(tempFile)
		assert.NilError(t, err)

		contentStr := string(content)
		assert.Assert(t, cmp.Contains(contentStr, "cd "))
		assert.Assert(t, cmp.Contains(contentStr, "worktrees/feature-a"))
		assert.Assert(t, cmp.Contains(contentStr, "echo "))
		assert.Assert(t, cmp.Contains(contentStr, "Switched to branch"))
	})
}

func TestWorktree_FallbackToCheckoutWhenNoWorktree(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Create main repo with two branches but no worktrees
		testutil.ExecOrFail(t, `
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
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "feature-a", "--parent=main"), 0)

		// Try to switch to feature-a (should fall back to normal checkout)
		tempFile := filepath.Join(t.TempDir(), "shell-exec")
		t.Setenv("YAS_SHELL_EXEC", tempFile)

		assert.Equal(t, yascli.Run("branch", "feature-a"), 0)

		// Verify we're on feature-a branch now (normal checkout happened)
		output := mustExecOutput("git", "branch", "--show-current")
		assert.Equal(t, strings.TrimSpace(output), "feature-a")

		// Verify the temp file is empty or doesn't exist (no cd command was written)
		content, err := os.ReadFile(tempFile)
		if err == nil {
			assert.Equal(t, string(content), "")
		}
		// If file doesn't exist, that's also fine - it means nothing was written
	})
}

func TestWorktree_ErrorWhenHookNotInstalled(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Create main repo with worktree
		testutil.ExecOrFail(t, `
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
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "feature-a", "--parent=main"), 0)

		// Create a worktree for feature-a
		worktreePath := filepath.Join(".", "worktrees", "feature-a")
		testutil.ExecOrFail(t, "git worktree add "+worktreePath+" feature-a")

		// Ensure we're back on main
		testutil.ExecOrFail(t, "git checkout main")

		// Try to switch to feature-a WITHOUT YAS_SHELL_EXEC set
		_, stderr, err := testutil.CaptureOutput(func() {
			exitCode := yascli.Run("branch", "feature-a")
			assert.Equal(t, exitCode, 1)
		})

		assert.NilError(t, err)
		assert.Assert(t, cmp.Contains(stderr, "YAS_SHELL_EXEC environment variable not set"))
		assert.Assert(t, cmp.Contains(stderr, "install the yas shell hook"))
		assert.Assert(t, cmp.Contains(stderr, "yas hook bash"))
		assert.Assert(t, cmp.Contains(stderr, "yas hook zsh"))
	})
}

func TestHook_BashOutputsHookCode(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		stdout, stderr, err := testutil.CaptureOutput(func() {
			exitCode := yascli.Run("hook", "bash")
			assert.Equal(t, exitCode, 0)
		})

		assert.NilError(t, err)
		assert.Equal(t, stderr, "")

		// Verify the hook code contains expected elements
		assert.Assert(t, cmp.Contains(stdout, "yas() {"))
		assert.Assert(t, cmp.Contains(stdout, "YAS_SHELL_EXEC"))
		assert.Assert(t, cmp.Contains(stdout, "mktemp"))
		assert.Assert(t, cmp.Contains(stdout, "command yas"))
		assert.Assert(t, cmp.Contains(stdout, "source"))
		assert.Assert(t, cmp.Contains(stdout, "rm -f"))
	})
}

func TestHook_ZshOutputsHookCode(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		stdout, stderr, err := testutil.CaptureOutput(func() {
			exitCode := yascli.Run("hook", "zsh")
			assert.Equal(t, exitCode, 0)
		})

		assert.NilError(t, err)
		assert.Equal(t, stderr, "")

		// Verify the hook code contains expected elements
		assert.Assert(t, cmp.Contains(stdout, "yas() {"))
		assert.Assert(t, cmp.Contains(stdout, "YAS_SHELL_EXEC"))
		assert.Assert(t, cmp.Contains(stdout, "mktemp"))
		assert.Assert(t, cmp.Contains(stdout, "command yas"))
		assert.Assert(t, cmp.Contains(stdout, "source"))
		assert.Assert(t, cmp.Contains(stdout, "rm -f"))
	})
}

func TestWorktree_SwitchFromWorktreeToMainBranch(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Create main repo with a branch that has a worktree
		testutil.ExecOrFail(t, `
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
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "feature-a", "--parent=main"), 0)

		// Create a worktree for feature-a
		worktreePath := filepath.Join(".", "worktrees", "feature-a")
		testutil.ExecOrFail(t, "git worktree add "+worktreePath+" feature-a")

		// Get primary repo directory (current directory)
		primaryDir := mustExecOutput("pwd")

		// Now try to switch to main from within the worktree
		tempFile := filepath.Join(t.TempDir(), "shell-exec")
		t.Setenv("YAS_SHELL_EXEC", tempFile)

		// Change to worktree directory and run yas from there
		t.Chdir(worktreePath)

		// Run yas branch from worktree directory
		assert.Equal(t, yascli.Run("branch", "main"), 0)

		// Verify the temp file contains cd to primary repo and yas br command
		content, err := os.ReadFile(tempFile)
		assert.NilError(t, err)

		contentStr := string(content)
		assert.Assert(t, cmp.Contains(contentStr, "cd "))
		assert.Assert(t, cmp.Contains(contentStr, strings.TrimSpace(primaryDir)))
		assert.Assert(t, cmp.Contains(contentStr, "yas"))
		assert.Assert(t, cmp.Contains(contentStr, "br"))
		assert.Assert(t, cmp.Contains(contentStr, "main"))
	})
}

func TestWorktree_SwitchFromWorktreeToAnotherNonWorktreeBranch(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Create main repo with multiple branches
		testutil.ExecOrFail(t, `
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
		`)

		// Initialize yas
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "feature-a", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "feature-b", "--parent=main"), 0)

		// Create a worktree for feature-a only
		worktreePath := filepath.Join(".", "worktrees", "feature-a")
		testutil.ExecOrFail(t, "git worktree add "+worktreePath+" feature-a")

		// Get primary repo directory
		primaryDir := mustExecOutput("pwd")

		// Now try to switch to feature-b from within feature-a worktree
		tempFile := filepath.Join(t.TempDir(), "shell-exec")
		t.Setenv("YAS_SHELL_EXEC", tempFile)

		// Change to worktree directory and run yas from there
		t.Chdir(worktreePath)

		// Run yas branch from worktree directory
		assert.Equal(t, yascli.Run("branch", "feature-b"), 0)

		// Verify the temp file contains cd to primary repo and yas br command
		content, err := os.ReadFile(tempFile)
		assert.NilError(t, err)

		contentStr := string(content)
		assert.Assert(t, cmp.Contains(contentStr, "cd "))
		assert.Assert(t, cmp.Contains(contentStr, strings.TrimSpace(primaryDir)))
		assert.Assert(t, cmp.Contains(contentStr, "yas"))
		assert.Assert(t, cmp.Contains(contentStr, "br"))
		assert.Assert(t, cmp.Contains(contentStr, "feature-b"))
	})
}

func TestWorktree_CreateBranchWithWorktreeFlag(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Create main repo
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"
		`)

		// Initialize yas
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)

		// Set up shell exec env
		tempFile := filepath.Join(t.TempDir(), "shell-exec")
		t.Setenv("YAS_SHELL_EXEC", tempFile)

		// Create a new branch with --worktree flag
		assert.Equal(t, yascli.Run("branch", "feature-a", "--worktree"), 0)

		// Verify the worktree was created at the correct path
		worktreePath := filepath.Join(".", ".yas", "worktrees", "feature-a")
		info, err := os.Stat(worktreePath)
		assert.NilError(t, err)
		assert.Assert(t, info.IsDir())

		// Verify the branch was created
		output := mustExecOutput("git", "branch", "--list", "feature-a")
		assert.Assert(t, cmp.Contains(output, "feature-a"))

		// Verify the shell exec file contains cd command to the worktree
		content, err := os.ReadFile(tempFile)
		assert.NilError(t, err)

		contentStr := string(content)
		assert.Assert(t, cmp.Contains(contentStr, "cd "))
		assert.Assert(t, cmp.Contains(contentStr, ".yas/worktrees/feature-a"))
	})
}

func TestWorktree_CreateBranchWithWorktreeFlagAndPrefix(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Create main repo
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git config user.email "testuser@example.com"
			touch main
			git add main
			git commit -m "main-0"
		`)

		// Initialize yas with AutoPrefixBranch enabled
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main", "--auto-prefix-branch"), 0)

		// Set up shell exec env
		tempFile := filepath.Join(t.TempDir(), "shell-exec")
		t.Setenv("YAS_SHELL_EXEC", tempFile)

		// Create a new branch with --worktree flag
		assert.Equal(t, yascli.Run("branch", "feature-a", "--worktree"), 0)

		// Verify the worktree was created with unprefixed name
		worktreePath := filepath.Join(".", ".yas", "worktrees", "feature-a")
		info, err := os.Stat(worktreePath)
		assert.NilError(t, err)
		assert.Assert(t, info.IsDir())

		// Verify the branch was created with prefix
		output := mustExecOutput("git", "branch", "--list", "testuser/feature-a")
		assert.Assert(t, cmp.Contains(output, "testuser/feature-a"))

		// Verify the parent relationship was tracked
		assert.Equal(t, yascli.Run("stack"), 0)
	})
}

func TestWorktree_CreateBranchWithWorktreeAndStagedChanges(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Create main repo
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"
		`)

		// Initialize yas
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)

		// Set up shell exec env
		tempFile := filepath.Join(t.TempDir(), "shell-exec")
		t.Setenv("YAS_SHELL_EXEC", tempFile)

		// Create a staged change
		testutil.ExecOrFail(t, `
			touch staged-file
			git add staged-file
		`)

		// Create a new branch with --worktree flag
		assert.Equal(t, yascli.Run("branch", "feature-a", "--worktree"), 0)

		// Verify the worktree was created at the correct path
		worktreePath := filepath.Join(".", ".yas", "worktrees", "feature-a")
		info, err := os.Stat(worktreePath)
		assert.NilError(t, err)
		assert.Assert(t, info.IsDir())

		// Verify the branch was created
		output := mustExecOutput("git", "branch", "--list", "feature-a")
		assert.Assert(t, cmp.Contains(output, "feature-a"))

		// Verify the staged changes were committed
		output = mustExecOutput("git", "log", "--oneline", "feature-a")
		assert.Assert(t, cmp.Contains(output, "Add staged changes"))

		// Verify the shell exec file contains cd command to the worktree
		content, err := os.ReadFile(tempFile)
		assert.NilError(t, err)
		contentStr := string(content)
		assert.Assert(t, cmp.Contains(contentStr, "cd "))
		assert.Assert(t, cmp.Contains(contentStr, ".yas/worktrees/feature-a"))
	})
}

func TestWorktree_CreateWorktreeForExistingBranch(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		// Create main repo with an existing branch
		testutil.ExecOrFail(t, `
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
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "feature-a", "--parent=main"), 0)

		// Set up shell exec env
		tempFile := filepath.Join(t.TempDir(), "shell-exec")
		t.Setenv("YAS_SHELL_EXEC", tempFile)

		// Create worktree for existing branch with --worktree flag
		assert.Equal(t, yascli.Run("branch", "feature-a", "--worktree"), 0)

		// Verify worktree WAS created
		worktreePath := filepath.Join(".", ".yas", "worktrees", "feature-a")
		info, err := os.Stat(worktreePath)
		assert.NilError(t, err)
		assert.Assert(t, info.IsDir())

		// Verify the shell exec file contains cd command to the worktree
		content, err := os.ReadFile(tempFile)
		assert.NilError(t, err)
		contentStr := string(content)
		assert.Assert(t, cmp.Contains(contentStr, "cd "))
		assert.Assert(t, cmp.Contains(contentStr, ".yas/worktrees/feature-a"))
	})
}
