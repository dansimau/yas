package gitexec

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dansimau/yas/pkg/fsutil"
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

// LinkedWorktreePathForBranch finds the worktree path for a given branch.
// Returns empty string if no worktree exists for the branch.
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

// WorktreeAdd creates a new worktree at the specified path with a new branch.
func (r *Repo) WorktreeAdd(path, branchName, startPoint string) error {
	return r.run("git", "worktree", "add", "-b", branchName, path, startPoint)
}

// WorktreeAddExisting creates a worktree for an existing branch.
func (r *Repo) WorktreeAddExisting(path, branchName string) error {
	return r.run("git", "worktree", "add", path, branchName)
}

// WorktreeRemove removes a worktree at the specified path.
// If force is true, it will remove the worktree even if it has uncommitted changes.
func (r *Repo) WorktreeRemove(worktreePath string, force bool) error {
	if force {
		return r.run("git", "worktree", "remove", worktreePath, "--force")
	}

	return r.run("git", "worktree", "remove", worktreePath)
}
