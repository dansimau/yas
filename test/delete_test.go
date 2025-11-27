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

// resolvePath resolves symlinks in a path (e.g. /var -> /private/var on macOS)
func resolvePath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}

	return resolved
}

func TestDelete_DeletesBranchByName(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create repo with a branch
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

	// Delete the branch with --force to skip confirmation
	result := cli.Run("delete", "feature-a", "--force")
	assert.NilError(t, result.Err())
	assert.Assert(t, cmp.Contains(result.Stdout(), "Deleted branch 'feature-a'"))

	// Verify branch is deleted
	output := mustExecOutput(tempDir, "git", "branch", "--list", "feature-a")
	assert.Equal(t, strings.TrimSpace(output), "")

	// Verify branch is marked as deleted in yas state
	result = cli.Run("list")
	assert.NilError(t, result.Err())
	assert.Assert(t, !cmp.Contains(result.Stdout(), "feature-a")().Success())
}

func TestDelete_DeletesCurrentBranchWithForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create repo with a branch and stay on it
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

	// Initialize yas
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())

	// Verify we're on feature-a
	output := mustExecOutput(tempDir, "git", "branch", "--show-current")
	assert.Equal(t, strings.TrimSpace(output), "feature-a")

	// Delete the current branch with --force (no branch name = current)
	result := cli.Run("delete", "--force")
	assert.NilError(t, result.Err())
	assert.Assert(t, cmp.Contains(result.Stdout(), "Deleted branch 'feature-a'"))

	// Verify we switched to trunk
	output = mustExecOutput(tempDir, "git", "branch", "--show-current")
	assert.Equal(t, strings.TrimSpace(output), "main")

	// Verify branch is deleted
	output = mustExecOutput(tempDir, "git", "branch", "--list", "feature-a")
	assert.Equal(t, strings.TrimSpace(output), "")
}

func TestDelete_RefusesToDeleteTrunkBranch(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create repo
	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"
	`)

	// Initialize yas
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())

	// Try to delete trunk branch
	result := cli.Run("delete", "main", "--force")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, cmp.Contains(result.Stderr(), "cannot delete trunk branch"))
}

func TestDelete_DeletesBranchWithWorktree(t *testing.T) {
	t.Parallel()

	tempDir := resolvePath(t.TempDir())

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create repo with a branch and worktree
	worktreePath := filepath.Join(tempDir, ".yas", "worktrees", "feature-a")
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

	// Verify worktree exists
	info, err := os.Stat(worktreePath)
	assert.NilError(t, err)
	assert.Assert(t, info.IsDir())

	// Initialize yas
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())

	// Delete the branch with --force
	result := cli.Run("delete", "feature-a", "--force")
	assert.NilError(t, result.Err())
	assert.Assert(t, cmp.Contains(result.Stdout(), "Deleted branch 'feature-a' and worktree at "+worktreePath))

	// Verify branch is deleted
	output := mustExecOutput(tempDir, "git", "branch", "--list", "feature-a")
	assert.Equal(t, strings.TrimSpace(output), "")

	// Verify worktree is deleted
	_, err = os.Stat(worktreePath)
	assert.Assert(t, os.IsNotExist(err))
}

func TestDelete_FailsWithDirtyWorktreeWithoutForce(t *testing.T) {
	t.Parallel()

	tempDir := resolvePath(t.TempDir())

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create repo with a branch and worktree
	worktreePath := filepath.Join(tempDir, ".yas", "worktrees", "feature-a")
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

	// Create uncommitted changes in the worktree
	testutil.ExecOrFail(t, worktreePath, `
		echo "dirty" > dirty-file
	`)

	// Initialize yas
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())

	// Try to delete without --force (simulate bypassing confirmation with stdin)
	cliWithStdin := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithStdin(strings.NewReader("y\n")),
	)
	result := cliWithStdin.Run("delete", "feature-a")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, cmp.Contains(result.Stderr(), "failed to remove worktree"))

	// Verify branch still exists
	output := mustExecOutput(tempDir, "git", "branch", "--list", "feature-a")
	assert.Assert(t, cmp.Contains(output, "feature-a"))

	// Verify worktree still exists
	info, err := os.Stat(worktreePath)
	assert.NilError(t, err)
	assert.Assert(t, info.IsDir())
}

func TestDelete_DeletesDirtyWorktreeWithForce(t *testing.T) {
	t.Parallel()

	tempDir := resolvePath(t.TempDir())

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create repo with a branch and worktree
	worktreePath := filepath.Join(tempDir, ".yas", "worktrees", "feature-a")
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

	// Create uncommitted changes in the worktree
	testutil.ExecOrFail(t, worktreePath, `
		echo "dirty" > dirty-file
	`)

	// Initialize yas
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())

	// Delete with --force should succeed even with dirty worktree
	result := cli.Run("delete", "feature-a", "--force")
	assert.NilError(t, result.Err())
	assert.Assert(t, cmp.Contains(result.Stdout(), "Deleted branch 'feature-a' and worktree at "+worktreePath))

	// Verify branch is deleted
	output := mustExecOutput(tempDir, "git", "branch", "--list", "feature-a")
	assert.Equal(t, strings.TrimSpace(output), "")

	// Verify worktree is deleted
	_, err := os.Stat(worktreePath)
	assert.Assert(t, os.IsNotExist(err))
}

func TestDelete_ConfirmationPromptWithoutForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create repo with a branch
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

	// Set up yas first without stdin
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())

	// Now test confirming "y" deletes the branch with a fresh cli that has stdin
	cliWithStdin := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithStdin(strings.NewReader("y\n")),
	)

	result := cliWithStdin.Run("delete", "feature-a")
	assert.NilError(t, result.Err())
	assert.Assert(t, cmp.Contains(result.Stdout(), "Delete branch 'feature-a'? (y/N)"))
	assert.Assert(t, cmp.Contains(result.Stdout(), "Deleted branch 'feature-a'"))

	// Verify branch is deleted
	output := mustExecOutput(tempDir, "git", "branch", "--list", "feature-a")
	assert.Equal(t, strings.TrimSpace(output), "")
}

func TestDelete_ConfirmationPromptDeclined(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create repo with a branch
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

	// Set up yas first without stdin
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())

	// Test declining with "n" does not delete the branch
	cliWithStdin := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithStdin(strings.NewReader("n\n")),
	)

	result := cliWithStdin.Run("delete", "feature-a")
	assert.NilError(t, result.Err())
	assert.Assert(t, cmp.Contains(result.Stdout(), "Delete branch 'feature-a'? (y/N)"))
	assert.Assert(t, !cmp.Contains(result.Stdout(), "Deleted branch")().Success())

	// Verify branch still exists
	output := mustExecOutput(tempDir, "git", "branch", "--list", "feature-a")
	assert.Assert(t, cmp.Contains(output, "feature-a"))
}

func TestDelete_ConfirmationShowsWorktreePath(t *testing.T) {
	t.Parallel()

	tempDir := resolvePath(t.TempDir())

	// Create repo with a branch and worktree
	worktreePath := filepath.Join(tempDir, ".yas", "worktrees", "feature-a")
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

	// Set up yas first without stdin
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature-a", "--parent=main").Err())

	// Decline deletion to verify the prompt message
	cliWithStdin := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithStdin(strings.NewReader("n\n")),
	)

	result := cliWithStdin.Run("delete", "feature-a")
	assert.NilError(t, result.Err())
	// Verify the prompt includes the worktree path
	assert.Assert(t, cmp.Contains(result.Stdout(), "Delete branch 'feature-a' at "+worktreePath+"? (y/N)"))
}
