package gitexec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dansimau/yas/pkg/fsutil"
	"github.com/dansimau/yas/pkg/xexec"
)

func (r *Repo) PrimaryWorktreePath() (string, error) {
	s, err := r.output("git", "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}

	gitCommonDir := s
	// Convert to absolute path if it's relative
	if !filepath.IsAbs(gitCommonDir) {
		gitCommonDir = filepath.Join(r.path, gitCommonDir)
	}

	return filepath.Dir(gitCommonDir), nil
}

// WorktreeEntry represents a single worktree entry.
type WorktreeEntry struct {
	Path   string
	Head   string
	Branch string
}

// Worktrees returns all worktrees for the repository.
func (r *Repo) Worktrees() ([]WorktreeEntry, error) {
	output, err := r.output("git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees (ensure you are on a recent version of git that supports worktrees): %w", err)
	}

	var (
		worktrees []WorktreeEntry
		current   WorktreeEntry
	)

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if current.Path != "" {
				worktrees = append(worktrees, current)
				current = WorktreeEntry{}
			}

			continue
		}

		switch {
		case strings.HasPrefix(line, "worktree "):
			current.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			current.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			branchRef := strings.TrimPrefix(line, "branch ")
			// Extract branch name from refs/heads/branch-name
			current.Branch = strings.TrimPrefix(branchRef, "refs/heads/")
		}
	}

	// Add last entry if exists
	if current.Path != "" {
		worktrees = append(worktrees, current)
	}

	return worktrees, nil
}

// LinkedWorktrees returns all linked/child worktrees for the repository (i.e. excluding the primary worktree).
func (r *Repo) LinkedWorktrees() ([]WorktreeEntry, error) {
	primaryWorktreePath, err := r.PrimaryWorktreePath()
	if err != nil {
		return nil, err
	}

	worktrees, err := r.Worktrees()
	if err != nil {
		return nil, err
	}

	linkedWorktrees := []WorktreeEntry{}

	for _, wt := range worktrees {
		isSameRealPath, err := fsutil.IsSameRealPath(wt.Path, primaryWorktreePath)
		if err != nil {
			if os.IsNotExist(err) {
				// Stale/prunable worktree entry whose path no longer exists
				// (e.g. git reports it as "prunable gitdir file points to
				// non-existent location"). There is nothing to operate on, so
				// skip it rather than aborting the whole operation.
				continue
			}

			return nil, err
		}

		if isSameRealPath {
			// Skip the primary worktree
			continue
		}

		linkedWorktrees = append(linkedWorktrees, wt)
	}

	return linkedWorktrees, nil
}

// LinkedWorktreePathForBranch finds the worktree path for a given branch in linked worktrees only.
// Returns empty string if no linked worktree exists for the branch.
// Also handles detached worktrees that have a rebase in progress for the target branch.
func (r *Repo) LinkedWorktreePathForBranch(branch string) (string, error) {
	worktrees, err := r.LinkedWorktrees()
	if err != nil {
		return "", err
	}

	for _, wt := range worktrees {
		if wt.Branch == branch {
			return wt.Path, nil
		}

		// Check if this is a detached worktree with a rebase in progress for our target branch
		if wt.Branch == "" {
			rebaseBranch, err := r.getRebaseBranchInWorktree(wt.Path)
			if err == nil && rebaseBranch == branch {
				return wt.Path, nil
			}
		}
	}

	return "", nil
}

// WorktreePathForBranch finds the worktree path for a given branch in ANY worktree (including primary).
// Returns empty string if no worktree exists for the branch.
// Also handles detached worktrees that have a rebase in progress for the target branch.
func (r *Repo) WorktreePathForBranch(branch string) (string, error) {
	worktrees, err := r.Worktrees()
	if err != nil {
		return "", err
	}

	for _, wt := range worktrees {
		if wt.Branch == branch {
			return wt.Path, nil
		}

		// Check if this is a detached worktree with a rebase in progress for our target branch
		if wt.Branch == "" {
			rebaseBranch, err := r.getRebaseBranchInWorktree(wt.Path)
			if err == nil && rebaseBranch == branch {
				return wt.Path, nil
			}
		}
	}

	return "", nil
}

// BranchHasWorktree checks if a branch is checked out in any worktree (including primary).
// Returns true if the branch has a worktree, false otherwise.
func (r *Repo) BranchHasWorktree(branch string) (bool, error) {
	worktrees, err := r.Worktrees()
	if err != nil {
		return false, err
	}

	for _, wt := range worktrees {
		if wt.Branch == branch {
			return true, nil
		}
	}

	return false, nil
}

// getRebaseBranchInWorktree checks if a rebase is in progress in the given worktree
// and returns the branch name being rebased.
func (r *Repo) getRebaseBranchInWorktree(worktreePath string) (string, error) {
	wtRepo := &Repo{path: worktreePath}

	gitDir, err := wtRepo.output("git", "rev-parse", "--git-dir")
	if err != nil {
		return "", err
	}

	// Check rebase-merge first (interactive rebase), then rebase-apply
	for _, rebaseDir := range []string{"rebase-merge", "rebase-apply"} {
		headNamePath := gitDir + "/" + rebaseDir + "/head-name"

		headName, err := wtRepo.output("cat", headNamePath)
		if err == nil {
			// head-name contains refs/heads/branch-name
			return strings.TrimPrefix(headName, "refs/heads/"), nil
		}
	}

	return "", errors.New("no rebase in progress")
}

// IsLinkedWorktree returns true if the current directory is a worktree (not the primary repo).
func (r *Repo) IsLinkedWorktree() (bool, error) {
	// In a worktree, .git is a file, not a directory
	// We can also check if git-common-dir differs from git-dir
	gitDir, err := r.output("git", "rev-parse", "--git-dir")
	if err != nil {
		return false, err
	}

	gitCommonDir, err := r.output("git", "rev-parse", "--git-common-dir")
	if err != nil {
		return false, err
	}

	// If they differ, we're in a worktree
	return gitDir != gitCommonDir, nil
}

// worktreeAdd runs `git worktree add` with git hooks disabled: creating a
// worktree runs post-checkout hooks (husky and friends), which is unwanted
// when worktrees are created programmatically, and slow when creating many.
func (r *Repo) worktreeAdd(args ...string) error {
	return r.run(append([]string{"git", "-c", "core.hooksPath=/dev/null", "worktree", "add", "--quiet"}, args...)...)
}

// WorktreeAdd creates a new worktree at the specified path with a new branch.
func (r *Repo) WorktreeAdd(path, branchName, startPoint string) error {
	return r.worktreeAdd("-b", branchName, path, startPoint)
}

// WorktreeAddExisting creates a worktree for an existing branch.
func (r *Repo) WorktreeAddExisting(path, branchName string) error {
	return r.worktreeAdd(path, branchName)
}

// WorktreeAddDetached creates a worktree at path with a detached HEAD at the
// given commit-ish.
func (r *Repo) WorktreeAddDetached(path, commitish string) error {
	return r.worktreeAdd("--detach", path, commitish)
}

// WorktreeAddTracking creates a worktree at path on a new local branch created
// from remoteRef (e.g. "origin/feature") and set up to track it.
func (r *Repo) WorktreeAddTracking(path, branchName, remoteRef string) error {
	configWriteMu.Lock()
	defer configWriteMu.Unlock()

	return r.worktreeAdd("--track", "-b", branchName, path, remoteRef)
}

// WorktreeRemove removes a worktree at the specified path.
// If force is true, it will remove the worktree even if it has uncommitted changes.
func (r *Repo) WorktreeRemove(worktreePath string, force bool) error {
	level := 0
	if force {
		level = 1
	}

	return r.WorktreeRemoveForce(worktreePath, level)
}

// WorktreeRemoveForce removes a worktree, passing --force the given number of
// times: once removes a worktree with uncommitted changes, twice also removes
// a locked worktree. Level 0 only removes clean worktrees.
func (r *Repo) WorktreeRemoveForce(worktreePath string, level int) error {
	args := []string{"git", "worktree", "remove", worktreePath}
	for range min(level, 2) {
		args = append(args, "--force")
	}

	return r.run(args...)
}

// WorktreePrune removes worktree administrative files for worktrees whose
// directories no longer exist.
func (r *Repo) WorktreePrune() error {
	return r.run("git", "worktree", "prune")
}

// CommonDir returns the absolute, symlink-resolved path of the repository's
// common git directory (the .git directory shared by all of its worktrees).
func (r *Repo) CommonDir() (string, error) {
	s, err := r.output("git", "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}

	if !filepath.IsAbs(s) {
		s = filepath.Join(r.path, s)
	}

	return filepath.EvalSymlinks(s)
}

// TopLevel returns the absolute path of the root of the working tree, or an
// error when the repository has no working tree (e.g. it is bare) or the
// directory is not inside a repository.
func (r *Repo) TopLevel() (string, error) {
	b, err := xexec.Command("git", "rev-parse", "--show-toplevel").
		WithEnvVars(CleanedGitEnv()).
		WithWorkingDir(r.path).
		WithStdout(nil).
		WithStderr(nil).
		Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(b)), nil
}
