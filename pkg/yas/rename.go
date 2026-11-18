package yas

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dansimau/yas/pkg/fsutil"
)

// RenameCurrentBranch renames the current branch in git and in the yas state,
// keeping its place in the stack: its metadata moves to the new name and its
// children are re-pointed at it. newName gets the same prefix treatment as a
// branch created with CreateBranch. It returns the old and new branch names.
//
// A worktree yas created for the branch moves to the path of the new name.
//
// Nothing is renamed on GitHub. GitHub has no way to move a pull request to
// another head branch (renaming the branch there closes the PR), so a branch
// with an open PR is refused unless force is set; the PR then stays behind on
// the old branch name and the next submit opens a new one.
func (yas *YAS) RenameCurrentBranch(newName string, force bool) (oldName string, fullNewName string, err error) {
	if err := yas.errIfRestackInProgress(); err != nil {
		return "", "", err
	}

	oldName, err = yas.git.GetCurrentBranchName()
	if err != nil {
		return "", "", fmt.Errorf("failed to get current branch: %w", err)
	}

	if oldName == yas.cfg.TrunkBranch {
		return "", "", errors.New("refusing to rename trunk branch")
	}

	fullNewName, err = yas.prefixedBranchName(newName)
	if err != nil {
		return "", "", err
	}

	if fullNewName == oldName {
		return "", "", fmt.Errorf("branch is already named '%s'", oldName)
	}

	if err := yas.git.CheckBranchName(fullNewName); err != nil {
		return "", "", err
	}

	exists, err := yas.git.BranchExists(fullNewName)
	if err != nil {
		return "", "", err
	}

	if exists {
		return "", "", fmt.Errorf("branch '%s' already exists", fullNewName)
	}

	tracked := yas.data.Branches.Exists(oldName)
	metadata := yas.data.Branches.Get(oldName)

	abandonedPR := ""

	if pr := metadata.GitHubPullRequest; pr.State == "OPEN" {
		if !force {
			return "", "", fmt.Errorf("branch '%s' has an open pull request (%s), which GitHub cannot move to a renamed branch\n\nUse --force to rename anyway: the pull request stays on '%s' and the next 'yas submit' opens a new one", oldName, pr.URL, oldName)
		}

		abandonedPR = pr.URL
	}

	if err := yas.git.RenameBranch(oldName, fullNewName); err != nil {
		return "", "", fmt.Errorf("failed to rename branch: %w", err)
	}

	if tracked {
		// Any PR belongs to the old branch name.
		metadata.GitHubPullRequest = PullRequestMetadata{}
		metadata.Name = fullNewName

		yas.data.Branches.Remove(oldName)
		yas.data.Branches.Set(fullNewName, metadata)
	}

	childPRsNeedNewBase := false

	for _, name := range yas.data.Branches.Names() {
		child := yas.data.Branches.Get(name)
		if child.Parent != oldName {
			continue
		}

		if pr := child.GitHubPullRequest; pr.State == "OPEN" && pr.BaseRefName == oldName {
			childPRsNeedNewBase = true
		}

		child.Parent = fullNewName
		yas.data.Branches.Set(name, child)
	}

	if err := yas.data.Save(); err != nil {
		// Don't leave git and the state disagreeing about the name.
		if undoErr := yas.git.RenameBranch(fullNewName, oldName); undoErr != nil {
			return "", "", fmt.Errorf("failed to save data: %w (and failed to rename branch back: %w)", err, undoErr)
		}

		return "", "", fmt.Errorf("failed to save data: %w", err)
	}

	// git carries the upstream over, so the new name would keep tracking (and
	// `git pull` from) the old remote branch. Drop it; the next submit pushes
	// the new name and tracks that.
	if yas.git.HasUpstream(fullNewName) {
		if err := yas.git.UnsetUpstream(fullNewName); err != nil {
			return "", "", fmt.Errorf("failed to unset upstream: %w", err)
		}

		fmt.Fprintf(os.Stderr, "NOTE: '%s' was already pushed; the remote branch keeps its old name and is no longer tracked\n", oldName)
	}

	if abandonedPR != "" {
		fmt.Fprintf(os.Stderr, "NOTE: %s is still open on '%s'; 'yas submit' will open a new pull request for '%s'\n", abandonedPR, oldName, fullNewName)
	}

	if childPRsNeedNewBase {
		fmt.Fprintf(os.Stderr, "NOTE: pull requests of child branches still have '%s' as their base; run 'yas submit --stack' to update them\n", oldName)
	}

	yas.moveWorktreeOfRenamedBranch(oldName, fullNewName)

	return oldName, fullNewName, nil
}

// moveWorktreeOfRenamedBranch moves the worktree of a branch that was just
// renamed to the path yas would create for the new name, and takes the shell
// along if it is inside. Only a worktree at the path yas created for the old
// name is moved; one the user placed elsewhere stays where it is. The branch
// rename has already happened, so problems are reported as warnings.
func (yas *YAS) moveWorktreeOfRenamedBranch(oldName, newName string) {
	if err := yas.tryMoveWorktreeOfRenamedBranch(oldName, newName); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: the branch was renamed, but its worktree was not moved: %v\n", err)
	}
}

func (yas *YAS) tryMoveWorktreeOfRenamedBranch(oldName, newName string) error {
	oldPath, err := yas.git.LinkedWorktreePathForBranch(newName)
	if err != nil || oldPath == "" {
		return err
	}

	primaryPath, err := yas.git.PrimaryWorktreePath()
	if err != nil {
		return err
	}

	if same, err := fsutil.IsSameRealPath(oldPath, filepath.Join(primaryPath, worktreePath, oldName)); err != nil || !same {
		return err
	}

	newPath := filepath.Join(primaryPath, worktreePath, newName)

	// Where the shell is inside the worktree, worked out before the directory
	// moves from under it.
	relDir, inside := "", false

	if cwd, err := os.Getwd(); err == nil {
		relDir, inside = relativeDirInside(oldPath, cwd)
	}

	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return err
	}

	if err := yas.git.WorktreeMove(oldPath, newPath); err != nil {
		return err
	}

	fmt.Printf("Moved worktree to %s\n", newPath)

	if !inside {
		return nil
	}

	newDir := filepath.Join(newPath, relDir)

	if ShellHook.ErrIfNotInstalled() != nil {
		fmt.Fprintf(os.Stderr, "NOTE: your shell is still in the old directory; run: cd %s\n", newDir)

		return nil
	}

	shellExec, err := ShellHook.NewWriter()
	if err != nil {
		return err
	}

	defer func() {
		if closeErr := shellExec.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "WARNING: failed to close shell exec file: %v\n", closeErr)
		}
	}()

	return shellExec.WriteCommand("cd", newDir)
}

// relativeDirInside returns the path of dir relative to root, and whether dir
// is root or inside it.
func relativeDirInside(root, dir string) (string, bool) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}

	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false
	}

	rel, err := filepath.Rel(realRoot, realDir)
	if err != nil || !filepath.IsLocal(rel) {
		return "", false
	}

	return rel, true
}
