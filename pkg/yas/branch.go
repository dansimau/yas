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

func (yas *YAS) cleanupBranch(name string) error {
	yas.data.Branches.Remove(name)

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
		if err := yas.cleanupBranch(name); err != nil {
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

	if err := yas.cleanupBranch(name); err != nil {
		return err
	}

	return nil
}

// DeleteMergedBranch deletes a merged branch after restacking its children onto its parent.
func (yas *YAS) DeleteMergedBranch(name string) error {
	// Get the metadata of the branch being deleted
	branchMetadata := yas.data.Branches.Get(name)
	parentBranch := branchMetadata.Parent

	// Require a parent branch for proper restacking
	if parentBranch == "" {
		return fmt.Errorf("branch %s has no parent branch set; cannot safely delete merged branch", name)
	}

	// Get the graph to find children
	graph, err := yas.graph()
	if err != nil {
		return fmt.Errorf("failed to get graph: %w", err)
	}

	// Find all children of this branch
	children, err := graph.GetChildren(name)
	if err != nil {
		return fmt.Errorf("failed to get children: %w", err)
	}

	// If there are children, restack them onto the parent
	if len(children) > 0 {
		fmt.Printf("Restacking %d child branch(es) onto %s...\n", len(children), parentBranch)

		for childID := range children {
			// Get child metadata for branch point
			childMetadata := yas.data.Branches.Get(childID)

			// Rebase the child onto the grandparent, removing commits from the merged branch
			// git rebase --onto <grandparent> <child's-branch-point> <child>
			// This replays only the child's commits (after its branch point) onto the grandparent
			fmt.Printf("  Rebasing %s onto %s...\n", childID, parentBranch)

			if err := yas.git.RebaseOntoWithBranchPoint(parentBranch, childMetadata.BranchPoint, childID); err != nil {
				return fmt.Errorf("failed to rebase %s onto %s: %w", childID, parentBranch, err)
			}

			// Update the child's parent to point to the grandparent
			childMetadata.Parent = parentBranch

			// Update the child's branch point to the grandparent's current commit
			grandparentCommit, err := yas.git.GetCommitHash(parentBranch)
			if err != nil {
				return fmt.Errorf("failed to get grandparent commit: %w", err)
			}

			childMetadata.BranchPoint = grandparentCommit

			yas.data.Branches.Set(childID, childMetadata)
		}

		// Save the updated metadata
		if err := yas.data.Save(); err != nil {
			return fmt.Errorf("failed to save updated metadata: %w", err)
		}
	}

	// Now delete the merged branch
	return yas.DeleteBranch(name)
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

	// Check if the branch has a worktree
	worktreePath, err := yas.git.WorktreeFindByBranch(branchName)
	if err != nil {
		// If we can't check for worktrees, just continue with normal checkout
		// (this might happen if git version is too old or other issues)
		fmt.Fprintf(os.Stderr, "WARNING: failed to check for worktrees: %v\n", err)
	} else if worktreePath != "" {
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

	// Check if we're currently in a worktree
	// We need to check based on the current working directory, not the repo directory
	// because the repo directory may have been resolved to the primary repo
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to get current directory: %v\n", err)
	}

	cwdRepo := gitexec.WithRepo(cwd)

	inWorktree, err := cwdRepo.IsWorktree()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to check if in worktree: %v\n", err)
	} else if inWorktree {
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
		primaryRepoPath, err := yas.git.WorktreeGetPrimaryRepoWorkingDirPath()
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
	return yas.data.Branches.ToSlice()
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
