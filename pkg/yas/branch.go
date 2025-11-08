package yas

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
)

func (yas *YAS) markBranchDeleted(name string) error {
	branchMetdata := yas.data.Branches.Get(name)

	now := time.Now()
	branchMetdata.Deleted = &now

	yas.data.Branches.Set(name, branchMetdata)

	return yas.data.Save()
}

func (yas *YAS) BranchExists(branchName string) (bool, error) {
	exists, err := yas.branchExistsLocally(branchName)
	if err != nil {
		return false, err
	}

	if exists {
		return true, nil
	}

	return yas.branchExistsRemotely(branchName)
}

func (yas *YAS) branchExistsLocally(branchName string) (bool, error) {
	return yas.git.BranchExists(branchName)
}

func (yas *YAS) branchExistsRemotely(branchName string) (bool, error) {
	return yas.git.RemoteBranchExists(branchName)
}

func (yas *YAS) DeleteBranch(name string) error {
	branchExists, err := yas.git.BranchExists(name)
	if err != nil {
		return err
	}

	if !branchExists {
		if err := yas.markBranchDeleted(name); err != nil {
			return err
		}

		return nil
	}

	currentBranchName, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return err
	}

	// Can't delete the branch while we're on it; switch to trunk
	if currentBranchName == name {
		if err := yas.git.QuietCheckout(yas.cfg.TrunkBranch); err != nil {
			return fmt.Errorf("can't delete branch while on it; failed to checkout trunk: %w", err)
		}
	}

	if err := yas.git.DeleteBranch(name); err != nil {
		return err
	}

	if err := yas.markBranchDeleted(name); err != nil {
		return err
	}

	return nil
}

func (yas *YAS) SetParent(branchName, parentBranchName, branchPoint string) error {
	if branchName == "" {
		currentBranch, err := yas.git.GetCurrentBranchName()
		if err != nil {
			return err
		}

		branchName = currentBranch
	}

	if branchName == yas.cfg.TrunkBranch {
		return errors.New("refusing to add trunk branch as a child")
	}

	if parentBranchName == "" {
		forkPoint, err := yas.git.GetForkPoint(branchName)
		if err != nil {
			return err // TODO return typed err
		}

		if forkPoint == "" {
			return errors.New("failed to autodetect parent branch (specify --parent)") // TODO type err
		}

		branchName, err := yas.git.GetLocalBranchNameForCommit(forkPoint + "^")
		if err != nil {
			return err // TODO return typed err
		}

		if branchName == "" {
			return errors.New("failed to autodetect parent branch (specify --parent)") // TODO type err
		}

		parentBranchName = branchName
	}

	branchMetdata := yas.data.Branches.Get(branchName)
	branchMetdata.Parent = parentBranchName

	// Capture the branch point - this is where the branch actually diverged from its parent
	if branchPoint == "" {
		// Autodetect: Use merge-base to find the common ancestor, which is the true branch point
		var err error

		branchPoint, err = yas.git.GetMergeBase(branchName, parentBranchName)
		if err != nil {
			return fmt.Errorf("failed to get branch point: %w", err)
		}
	}

	branchMetdata.BranchPoint = branchPoint

	// Initialize Created timestamp if not already set
	if branchMetdata.Created.IsZero() {
		branchMetdata.Created = time.Now()
	}

	// Undelete it if it was previously deleted
	branchMetdata.Deleted = nil

	yas.data.Branches.Set(branchName, branchMetdata)

	if err := yas.data.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to save data: %v\n", err)

		return fmt.Errorf("failed to save data: %w", err)
	}

	shortHash, err := yas.git.GetShortHash(branchPoint)
	if err != nil {
		return fmt.Errorf("failed to get short hash: %w", err)
	}

	fmt.Printf("Set '%s' as parent of '%s' (branched after %s)\n", parentBranchName, branchName, shortHash)

	return nil
}

// SwitchBranchInteractive shows an interactive selector and switches to the chosen branch.
func (yas *YAS) SwitchBranchInteractive() error {
	// Get current branch to pre-select it
	currentBranch, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	// Get the list of branches
	items, err := yas.GetBranchList(false, false)
	if err != nil {
		return fmt.Errorf("failed to get branch list: %w", err)
	}

	if len(items) == 0 {
		return errors.New("no branches found")
	}

	// Find the index of the current branch
	initialCursor := 0

	for i, item := range items {
		if item.ID == currentBranch {
			initialCursor = i

			break
		}
	}

	// Show interactive selector
	selected, err := InteractiveSelect(items, initialCursor, "Choose branch to switch to:")
	if err != nil {
		return fmt.Errorf("selection failed: %w", err)
	}

	// User cancelled
	if selected == nil {
		return nil
	}

	// Check out the selected branch
	if err := yas.git.Checkout(selected.ID); err != nil {
		return fmt.Errorf("failed to checkout branch: %w", err)
	}

	return nil
}

func (yas *YAS) SwitchBranch(branchName string) error {
	// Check if the branch exists locally
	localBranchExists, err := yas.branchExistsLocally(branchName)
	if err != nil {
		return fmt.Errorf("failed to check if branch exists locally: %w", err)
	}

	// Call on git to switch to the branch
	if err := yas.git.Checkout(branchName); err != nil {
		return fmt.Errorf("failed to checkout branch: %w", err)
	}

	// If the branch did not previously exist locally, refresh it so we can
	// track it and get the latest PR status.
	if !localBranchExists {
		if err := yas.RefreshRemoteStatus(branchName); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: failed to refresh remote status for branch: %v\n", err)
		}
	}

	return nil
}

func (yas *YAS) TrackedBranches() Branches {
	return yas.data.Branches.ToSlice().NotDeleted()
}

func (yas *YAS) UntrackedBranches() ([]string, error) {
	iter, err := yas.repo.Branches()
	if err != nil {
		return nil, err
	}

	branches := []string{}

	if err := iter.ForEach(func(r *plumbing.Reference) error {
		name := r.Name().Short()
		if !yas.data.Branches.Exists(name) {
			branches = append(branches, name)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return branches, nil
}

// pruneMetadata removes old branches from the metadata file.
func (yas *YAS) pruneMetadata() error {
	removed := false

	for _, branch := range yas.data.Branches.ToSlice().WithCreatedDateBefore(time.Now().Add(-24 * time.Hour * 7)) {
		if strings.TrimSpace(branch.Name) == "" {
			continue
		}

		exists, err := yas.git.BranchExists(branch.Name)
		if err != nil {
			return err
		}

		if exists {
			continue
		}

		yas.data.Branches.Remove(branch.Name)

		removed = true
	}

	if !removed {
		return nil
	}

	return yas.data.Save()
}
