package yas

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dansimau/yas/pkg/fsutil"
	"github.com/dansimau/yas/pkg/gitexec"
)

// restackRepoDir returns the worktree this restack works in, for
// RestackState.RepoDir.
func (yas *YAS) restackRepoDir() string {
	dir, err := filepath.Abs(yas.git.Path())
	if err != nil {
		return ""
	}

	return dir
}

// useRestackRepoDir makes yas work in the worktree the restack was started
// in, so that continuing or aborting from another worktree (such as the one a
// rebase stopped in) checks out branches in the same place as the restack did.
func (yas *YAS) useRestackRepoDir(state *RestackState) {
	if state.RepoDir == "" {
		return
	}

	if _, err := os.Stat(state.RepoDir); err != nil {
		return
	}

	yas.git = gitexec.WithRepo(state.RepoDir)
}

// stopAtRebase is called when a restack has stopped on a rebase in the
// worktree at dir, and returns the error to report (cause, plus where the
// rebase is when that is not where the user is).
//
// A rebase can stop in a different worktree from the one the user ran yas in,
// where `git status` shows nothing of it. In that case the shell is moved to
// dir (through the shell hook) and the directory it came from is recorded in
// state, so that `yas continue` and `yas abort` can move it back.
func (yas *YAS) stopAtRebase(state *RestackState, dir string, cause error) error {
	if cwdIsInWorktree(dir) {
		return cause
	}

	if state.ReturnDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			state.ReturnDir = cwd

			if err := yas.saveRestackState(state); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to save restack state: %v\n", err)
			}
		}
	}

	if ShellHook.ErrIfNotInstalled() != nil {
		return fmt.Errorf("%w\n\nThe rebase stopped in %s\n(install the yas shell hook to be moved there automatically, see 'yas hook')", cause, dir)
	}

	if err := changeShellDir(dir, "Moved to "+dir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	return fmt.Errorf("%w\n\nThe rebase stopped in %s", cause, dir)
}

// returnFromRebase moves the shell back to the directory it was in before
// stopAtRebase moved it (if it did), once a restack has finished or has been
// aborted. It does nothing without the shell hook or when the directory no
// longer exists.
func returnFromRebase(state *RestackState) {
	dir := state.ReturnDir
	if dir == "" || ShellHook.ErrIfNotInstalled() != nil {
		return
	}

	if _, err := os.Stat(dir); err != nil {
		return
	}

	if cwd, err := os.Getwd(); err == nil {
		if same, err := fsutil.IsSameRealPath(cwd, dir); err == nil && same {
			return
		}
	}

	if err := changeShellDir(dir, "Returned to "+dir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}
}

// cwdIsInWorktree reports whether the working directory is inside the
// worktree whose root is dir (and not inside another worktree nested in it).
func cwdIsInWorktree(dir string) bool {
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}

	topLevel, err := gitexec.WithRepo(cwd).TopLevel()
	if err != nil {
		return false
	}

	same, err := fsutil.IsSameRealPath(topLevel, dir)

	return err == nil && same
}

// changeShellDir makes the shell hook change the shell's directory to dir and
// print message.
func changeShellDir(dir, message string) error {
	shellExec, err := ShellHook.NewWriter()
	if err != nil {
		return err
	}

	if err := shellExec.WriteCommand("cd", dir); err != nil {
		_ = shellExec.Close()

		return fmt.Errorf("failed to write cd command: %w", err)
	}

	if err := shellExec.WriteCommand("echo", message); err != nil {
		_ = shellExec.Close()

		return fmt.Errorf("failed to write echo command: %w", err)
	}

	if err := shellExec.Close(); err != nil {
		return fmt.Errorf("failed to close shell exec file: %w", err)
	}

	return nil
}
