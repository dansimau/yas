package yas

import (
	"fmt"
	"path"
	"path/filepath"
)

const worktreePath = ".yas/worktrees"

// WorktreePathForBranch returns the worktree path for a branch, or empty string if none exists.
func (yas *YAS) WorktreePathForBranch(branchName string) (string, error) {
	return yas.git.LinkedWorktreePathForBranch(branchName)
}

// EnsureLinkedWorktreeForBranch creates a worktree for an existing branch.
// The worktree is created at the specified path relative to the repo root.
// After creation, it switches to the worktree using SwitchBranch.
func (yas *YAS) EnsureLinkedWorktreeForBranch(branchName string) error {
	// Check if worktree already exists for this branch
	existingWorktreePath, err := yas.git.LinkedWorktreePathForBranch(branchName)
	if err != nil {
		return fmt.Errorf("failed to check for existing worktree: %w", err)
	}

	// Check if the existing worktree is the primary repo (not a separate worktree)
	// Use filepath.EvalSymlinks to resolve symlinks like /private/tmp -> /tmp on macOS
	existingResolved, err := filepath.EvalSymlinks(existingWorktreePath)
	if err != nil {
		existingResolved = existingWorktreePath
	}

	repoResolved, err := filepath.EvalSymlinks(yas.cfg.RepoDirectory)
	if err != nil {
		repoResolved = yas.cfg.RepoDirectory
	}

	// Worktree already exists (and it's not the primary repo)
	if existingWorktreePath != "" && existingResolved != repoResolved {
		return nil
	}

	// If we're currently on the branch, switch off it first
	// (git doesn't allow creating a worktree for a checked-out branch)
	currentBranch, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	if currentBranch == branchName {
		// Get the parent branch to switch to
		branchMetadata := yas.data.Branches.Get(branchName)

		parentBranch := yas.cfg.TrunkBranch // default to trunk
		if branchMetadata.Parent != "" {
			parentBranch = branchMetadata.Parent
		}

		// Switch to parent branch
		if err := yas.git.QuietCheckout(parentBranch); err != nil {
			return fmt.Errorf("failed to switch off branch before creating worktree: %w", err)
		}
	}

	// Create the worktree for existing branch (use primary worktree path to avoid nesting)
	primaryWorktreePath, err := yas.git.PrimaryWorktreePath()
	if err != nil {
		return fmt.Errorf("failed to get primary worktree path: %w", err)
	}

	fullWorktreePath := path.Join(primaryWorktreePath, worktreePath, branchName)
	if err := yas.git.WorktreeAddExisting(fullWorktreePath, branchName); err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	return nil
}
