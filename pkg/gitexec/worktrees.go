package gitexec

import (
	"fmt"
	"path/filepath"
	"strings"
)

func (r *Repo) WorktreeGetPrimaryRepoPath() (string, error) {
	s, err := r.output("git", "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}

	return s, nil
}

func (r *Repo) WorktreeGetPrimaryRepoWorkingDirPath() (string, error) {
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

// WorktreeList returns all worktrees for the repository.
func (r *Repo) WorktreeList() ([]WorktreeEntry, error) {
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

// WorktreeFindByBranch finds the worktree path for a given branch
// Returns empty string if no worktree exists for the branch.
func (r *Repo) WorktreeFindByBranch(branch string) (string, error) {
	worktrees, err := r.WorktreeList()
	if err != nil {
		return "", err
	}

	for _, wt := range worktrees {
		if wt.Branch == branch {
			return wt.Path, nil
		}
	}

	return "", nil
}

// IsWorktree returns true if the current directory is a worktree (not the primary repo).
func (r *Repo) IsWorktree() (bool, error) {
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
