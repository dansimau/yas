package yas

import (
	"errors"
	"fmt"
)

// Move rebases the current branch and all its descendants onto a new parent branch.
func (yas *YAS) Move(targetBranch string) error {
	return yas.MoveBranch("", targetBranch)
}

// MoveBranch rebases the specified branch and all its descendants onto a new parent branch.
func (yas *YAS) MoveBranch(branchName, targetBranch string) error {
	// Check if a restack is already in progress
	if err := yas.errIfRestackInProgress(); err != nil {
		return err
	}

	if branchName == "" {
		currentBranch, err := yas.git.GetCurrentBranchName()
		if err != nil {
			return err
		}

		branchName = currentBranch
	}

	// Can't move trunk branch
	if branchName == yas.cfg.TrunkBranch {
		return errors.New("cannot move trunk branch")
	}

	// Verify target branch exists
	exists, err := yas.git.BranchExists(targetBranch)
	if err != nil {
		return fmt.Errorf("failed to check if target branch exists: %w", err)
	}

	if !exists {
		return fmt.Errorf("target branch '%s' does not exist", targetBranch)
	}

	// Get branch metadata
	currentMetadata := yas.data.Branches.Get(branchName)
	if currentMetadata.BranchPoint == "" {
		return fmt.Errorf("branch point is not set for %s (run 'yas add' first)", branchName)
	}

	// Update metadata to point to the new parent
	currentMetadata.Parent = targetBranch
	yas.data.Branches.Set(branchName, currentMetadata)

	if err := yas.data.Save(); err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	// Now rebase this branch and all children
	return yas.Restack(branchName, false)
}
