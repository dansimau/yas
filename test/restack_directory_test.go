package test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

// setupConflictInWorktree creates a repo in which restacking topic-a (checked
// out in a linked worktree) onto main conflicts. It returns the primary repo
// and worktree paths with symlinks resolved, as git reports them.
func setupConflictInWorktree(t *testing.T) (repoDir, worktreePath string) {
	t.Helper()

	repoDir, err := filepath.EvalSymlinks(t.TempDir())
	assert.NilError(t, err)

	worktreePath = filepath.Join(repoDir, "worktrees", "topic-a")

	testutil.ExecOrFail(t, repoDir, `
		git init --initial-branch=main

		echo "line1" > file.txt
		git add file.txt
		git commit -m "main-0"

		git checkout -b topic-a
		echo "line2-from-a" >> file.txt
		git add file.txt
		git commit -m "topic-a-0"

		git checkout main
		git worktree add `+worktreePath+` topic-a

		echo "line2-from-main" >> file.txt
		git add file.txt
		git commit -m "main-1"
	`)

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go", gocmdtester.WithWorkingDir(repoDir))
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	return repoDir, worktreePath
}

// readAndReset returns what yas wrote for the shell hook and empties the file,
// as the hook uses a fresh file for every command.
func readAndReset(t *testing.T, shellExecPath string) string {
	t.Helper()

	content, err := os.ReadFile(shellExecPath)
	if os.IsNotExist(err) {
		return ""
	}

	assert.NilError(t, err)
	assert.NilError(t, os.Remove(shellExecPath))

	return string(content)
}

func TestRestack_ConflictInOtherWorktreeMovesShellThereAndBack(t *testing.T) {
	t.Parallel()

	repoDir, worktreePath := setupConflictInWorktree(t)
	shellExecPath := filepath.Join(t.TempDir(), "shell-exec")

	cliPrimary := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(repoDir),
		gocmdtester.WithEnv("YAS_SHELL_EXEC", shellExecPath),
	)
	cliWorktree := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(worktreePath),
		gocmdtester.WithEnv("YAS_SHELL_EXEC", shellExecPath),
	)

	// The rebase stops in topic-a's worktree, not where restack was run.
	result := cliPrimary.Run("restack", "--all")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, cmp.Contains(result.Stderr(), "The rebase stopped in "+worktreePath))
	assert.Assert(t, cmp.Contains(readAndReset(t, shellExecPath), "cd "+worktreePath+"\n"))

	// Resolve the conflict and continue from the worktree: the shell goes
	// back to where the restack was started.
	testutil.ExecOrFail(t, worktreePath, `
		printf 'line1\nline2-from-main\nline2-from-a\n' > file.txt
		git add file.txt
	`)

	assert.NilError(t, cliWorktree.Run("continue").Err())
	assert.Assert(t, cmp.Contains(readAndReset(t, shellExecPath), "cd "+repoDir+"\n"))
}

func TestRestack_AbortReturnsShellToStartingDirectory(t *testing.T) {
	t.Parallel()

	repoDir, worktreePath := setupConflictInWorktree(t)
	shellExecPath := filepath.Join(t.TempDir(), "shell-exec")

	// Start from a subdirectory to check the exact directory is restored.
	startDir := filepath.Join(repoDir, "sub")
	assert.NilError(t, os.Mkdir(startDir, 0o755))

	cliStart := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(startDir),
		gocmdtester.WithEnv("YAS_SHELL_EXEC", shellExecPath),
	)
	cliWorktree := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(worktreePath),
		gocmdtester.WithEnv("YAS_SHELL_EXEC", shellExecPath),
	)

	assert.Equal(t, cliStart.Run("restack", "--all").ExitCode(), 1)
	assert.Assert(t, cmp.Contains(readAndReset(t, shellExecPath), "cd "+worktreePath+"\n"))

	assert.NilError(t, cliWorktree.Run("abort").Err())
	assert.Assert(t, cmp.Contains(readAndReset(t, shellExecPath), "cd "+startDir+"\n"))
}

func TestRestack_ConflictInCurrentWorktreeDoesNotMoveShell(t *testing.T) {
	t.Parallel()

	_, worktreePath := setupConflictInWorktree(t)
	shellExecPath := filepath.Join(t.TempDir(), "shell-exec")

	cliWorktree := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(worktreePath),
		gocmdtester.WithEnv("YAS_SHELL_EXEC", shellExecPath),
	)

	result := cliWorktree.Run("restack")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, !cmp.Contains(result.Stderr(), "The rebase stopped in")().Success())
	assert.Equal(t, readAndReset(t, shellExecPath), "")

	assert.NilError(t, cliWorktree.Run("abort").Err())
	assert.Equal(t, readAndReset(t, shellExecPath), "")
}

func TestRestack_ConflictInOtherWorktreeWithoutShellHook(t *testing.T) {
	t.Parallel()

	repoDir, worktreePath := setupConflictInWorktree(t)

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go", gocmdtester.WithWorkingDir(repoDir))

	result := cli.Run("restack", "--all")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, cmp.Contains(result.Stderr(), "The rebase stopped in "+worktreePath))
	assert.Assert(t, cmp.Contains(result.Stderr(), "install the yas shell hook"))

	assert.NilError(t, cli.Run("abort").Err())
}
