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

// PrimaryRepoPath returns the path to the primary repository (main worktree).
func (yas *YAS) PrimaryRepoPath() (string, error) {
	return yas.git.PrimaryWorktreePath()
}

// EnsureLinkedWorktreeForBranch creates a worktree for an existing branch.
// The worktree is created at the specified path relative to the repo root.
// After creation, it switches to the worktree using SwitchBranch.
func (yas *YAS) EnsureLinkedWorktreeForBranch(branchName string) error {
	// Don't create a worktree for the trunk branch - it should stay in the primary worktree.
	// This also handles the case where trunk is already checked out in primary worktree,
	// which would otherwise cause "branch is already used by worktree" error.
	if branchName == yas.cfg.TrunkBranch {
		return nil
	}

	// Check if worktree already exists for this branch
	existingWorktreePath, err := yas.git.LinkedWorktreePathForBranch(branchName)
	if err != nil {
		return fmt.Errorf("failed to check for existing worktree: %w", err)
	}

	// Check if the found worktree is different from where we're running
	// Use filepath.EvalSymlinks to resolve symlinks like /private/tmp -> /tmp on macOS
	existingResolved, err := filepath.EvalSymlinks(existingWorktreePath)
	if err != nil {
		existingResolved = existingWorktreePath
	}

	repoResolved, err := filepath.EvalSymlinks(yas.cfg.RepoDirectory)
	if err != nil {
		repoResolved = yas.cfg.RepoDirectory
	}

	// Worktree already exists for this branch in a DIFFERENT location than current directory.
	// If the branch is just temporarily checked out in the current worktree (same as RepoDirectory),
	// we still want to create a dedicated worktree for it.
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
