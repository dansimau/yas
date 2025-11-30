package yas

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dansimau/yas/pkg/gitexec"
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
	exists, err := yas.BranchExistsLocally(branchName)
	if err != nil {
		return false, err
	}

	if exists {
		return true, nil
	}

	return yas.BranchExistsRemotely(branchName)
}

func (yas *YAS) BranchExistsLocally(branchName string) (bool, error) {
	return yas.git.BranchExists(branchName)
}

func (yas *YAS) BranchExistsRemotely(branchName string) (bool, error) {
	return yas.git.RemoteBranchExists(branchName)
}

// DeleteBranch deletes a branch and its associated worktree if one exists.
// If force is true, it will remove the worktree even if it has uncommitted changes.
func (yas *YAS) DeleteBranch(branchName string, force bool) error {
	branchExists, err := yas.git.BranchExists(branchName)
	if err != nil {
		return err
	}

	if !branchExists {
		return yas.markBranchDeleted(branchName)
	}

	currentBranch, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return err
	}

	worktreePath, err := yas.git.LinkedWorktreePathForBranch(branchName)
	if err != nil {
		return fmt.Errorf("failed to check for worktree: %w", err)
	}

	if worktreePath != "" {
		return yas.deleteWorktreeBranch(worktreePath, branchName, force)
	}

	return yas.deleteNonWorktreeBranch(branchName, currentBranch)
}

// deleteNonWorktreeBranch deletes a branch that does not have a worktree.
func (yas *YAS) deleteNonWorktreeBranch(branchName string, currentBranch string) error {
	// If deleting the current branch, we need to switch to trunk first
	if currentBranch == branchName {
		if err := yas.git.QuietCheckout(yas.cfg.TrunkBranch); err != nil {
			return err
		}
	}

	if err := yas.git.DeleteBranch(branchName); err != nil {
		return err
	}

	return yas.markBranchDeleted(branchName)
}

// deleteWorktreeBranch deletes a branch that has an associated worktree.
func (yas *YAS) deleteWorktreeBranch(worktreePath string, branchName string, force bool) error {
	primaryRepoPath, err := yas.git.PrimaryWorktreePath()
	if err != nil {
		return fmt.Errorf("failed to get primary repo path: %w", err)
	}

	deletingCurrentWorktree := worktreePath == yas.git.Path()

	// Verify shell exec is available before any destructive operations
	if deletingCurrentWorktree {
		if err := errIfShellHookNotInstalled(); err != nil {
			return err
		}
	}

	// Remove the worktree
	if err := yas.git.WorktreeRemove(worktreePath, force); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	// Delete the branch from primary repo dir
	if err := gitexec.WithRepo(primaryRepoPath).DeleteBranch(branchName); err != nil {
		return err
	}

	// Mark as deleted in metadata
	if err := yas.markBranchDeleted(branchName); err != nil {
		return err
	}

	// Switch to primary repo if we deleted the current worktree
	if deletingCurrentWorktree {
		return yas.switchDirectoryAfterDeletion(primaryRepoPath)
	}

	return nil
}

// switchDirectoryAfterDeletion uses ShellExecWriter to change the shell's
// working directory after a worktree has been deleted.
func (yas *YAS) switchDirectoryAfterDeletion(targetDir string) error {
	shellExec, err := NewShellExecWriter()
	if err != nil {
		return err
	}

	defer func() {
		if closeErr := shellExec.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "WARNING: failed to close shell exec file: %v\n", closeErr)
		}
	}()

	if err := shellExec.WriteCommand("cd", targetDir); err != nil {
		return fmt.Errorf("failed to write cd command: %w", err)
	}

	if err := shellExec.WriteCommand("echo", "Switched to main worktree"); err != nil {
		return fmt.Errorf("failed to write cd command: %w", err)
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

	branchMetdata := yas.data.Branches.Get(branchName)

	// See if we can get this from the branch metadata
	if parentBranchName == "" {
		parentBranchName = branchMetdata.GitHubPullRequest.BaseRefName
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

	branchMetdata.Parent = parentBranchName

	// Capture the branch point - this is where the branch actually diverged from its parent.
	// Try to autodetect: Use merge-base to find the common ancestor, which is the true branch point.
	if branchPoint == "" {
		var err error

		branchesToTry := []string{
			parentBranchName,
			// Handle case where we are checking out a remote-only branch and we don't have the parent locally
			"origin/" + parentBranchName,
		}

		for _, branch := range branchesToTry {
			branchPoint, err = yas.git.GetMergeBase(branchName, branch)
			if err != nil {
				continue
			}

			break
		}

		if branchPoint == "" {
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
	if err := yas.SwitchBranch(selected.ID); err != nil {
		return fmt.Errorf("failed to checkout branch: %w", err)
	}

	return nil
}

func (yas *YAS) SwitchBranch(branchName string) error {
	// Check if the branch has a worktree
	worktreePath, err := yas.git.LinkedWorktreePathForBranch(branchName)
	if err != nil {
		return fmt.Errorf("failed to check for linked worktree path for branch: %w", err)
	}

	if worktreePath != "" {
		// Branch has a worktree - switch to it using shell exec
		shellExec, err := NewShellExecWriter()
		if err != nil {
			return err
		}

		defer func() {
			if closeErr := shellExec.Close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "WARNING: failed to close shell exec file: %v\n", closeErr)
			}
		}()

		// Write cd command to change to worktree directory
		if err := shellExec.WriteCommand("cd", worktreePath); err != nil {
			return fmt.Errorf("failed to write cd command: %w", err)
		}

		// Write echo message to show successful switch
		message := fmt.Sprintf("Switched to branch '%s' in worktree: %s", branchName, worktreePath)
		if err := shellExec.WriteCommand("echo", message); err != nil {
			return fmt.Errorf("failed to write echo command: %w", err)
		}

		return nil
	}

	inWorktree, err := yas.git.IsLinkedWorktree()
	if err != nil {
		return fmt.Errorf("failed to check if in worktree: %w", err)
	}

	if inWorktree {
		// We're in a worktree but target branch doesn't have one
		// Switch back to primary repo and run checkout there
		shellExec, err := NewShellExecWriter()
		if err != nil {
			return err
		}

		defer func() {
			if closeErr := shellExec.Close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "WARNING: failed to close shell exec file: %v\n", closeErr)
			}
		}()

		// Get primary repo working directory
		primaryRepoPath, err := yas.git.PrimaryWorktreePath()
		if err != nil {
			return fmt.Errorf("failed to get primary repo path: %w", err)
		}

		// Write cd command to change to primary repo
		if err := shellExec.WriteCommand("cd", primaryRepoPath); err != nil {
			return fmt.Errorf("failed to write cd command: %w", err)
		}

		// Write command to run yas branch switch in primary repo
		if err := shellExec.WriteCommand("yas", "br", branchName); err != nil {
			return fmt.Errorf("failed to write yas command: %w", err)
		}

		return nil
	}

	// We're in primary repo and target has no worktree - proceed with normal checkout
	if err := yas.git.Checkout(branchName); err != nil {
		return fmt.Errorf("failed to checkout branch: %w", err)
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
