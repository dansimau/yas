package yas

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/heimdalr/dag"
)

var ErrRestackInProgress = errors.New("a restack operation is already in progress\n\nRun 'yas continue' to resume or 'yas abort' to cancel")

// errIfRestackInProgress returns an error if a restack operation is in progress.
func (yas *YAS) errIfRestackInProgress() error {
	exists, err := yas.restackStateExists()
	if err != nil {
		return err
	}

	if exists {
		return ErrRestackInProgress
	}

	return nil
}

func (yas *YAS) RestackInProgress() (bool, error) {
	return yas.restackStateExists()
}

// Restack rebases all branches starting from trunk, including all descendants
// and forks.
func (yas *YAS) Restack(branch string, dryRun bool) error {
	// Check if a restack is already in progress
	if err := yas.errIfRestackInProgress(); err != nil {
		return err
	}

	if branch == "" {
		branch = yas.cfg.TrunkBranch
	}

	// Remember the starting branch
	startingBranch, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return err
	}

	// Track which branches were rebased
	rebasedBranches := []string{}

	// Keep rebuilding and processing the work queue until no more work remains
	// This loop is necessary because completing one rebase may cause descendant branches to need rebasing
	for {
		graph, err := yas.graph()
		if err != nil {
			return err
		}

		// Build the work queue: a list of [child, parent] pairs to rebase
		var workQueue [][2]string

		// First, check if the starting branch itself needs rebasing (unless it's trunk)
		if branch != yas.cfg.TrunkBranch {
			metadata := yas.data.Branches.Get(branch)
			if metadata.Parent != "" {
				needsRebase, err := yas.needsRebase(branch, metadata.Parent)
				if err != nil {
					return err
				}

				if needsRebase {
					// Add the starting branch to the work queue
					workQueue = append(workQueue, [2]string{branch, metadata.Parent})
				}
			}
		}

		// Then add all descendants of the starting branch
		if err := yas.buildRestackWorkQueue(graph, branch, &workQueue); err != nil {
			return err
		}

		// No more work to do
		if len(workQueue) == 0 {
			break
		}

		if dryRun {
			fmt.Printf("Would restack %d branches:\n", len(workQueue))

			for _, work := range workQueue {
				fmt.Printf("  - %s -> %s\n", work[0], work[1])
			}

			break
		}

		// Write initial restack state
		if err := yas.saveRestackState(&RestackState{
			StartingBranch:  startingBranch,
			CurrentBranch:   workQueue[0][0],
			CurrentParent:   workQueue[0][1],
			RemainingWork:   workQueue,
			RebasedBranches: rebasedBranches,
		}); err != nil {
			return fmt.Errorf("rebase failed and unable to save restack state: %w", err)
		}

		// Process the work queue
		if err := yas.processRestackWorkQueue(startingBranch, workQueue, &rebasedBranches); err != nil {
			return err
		}
	}

	// Clean up any restack state file (in case it exists)
	if err := yas.deleteRestackState(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to delete restack state: %v\n", err)
	}

	// Return to the starting branch
	if err := yas.git.QuietCheckout(startingBranch); err != nil {
		return fmt.Errorf("restack succeeded but failed to return to branch %s: %w", startingBranch, err)
	}

	// Check if any rebased branches have PRs
	if len(rebasedBranches) > 0 {
		branchesWithPRs := []string{}

		for _, branchName := range rebasedBranches {
			metadata := yas.data.Branches.Get(branchName)
			if metadata.GitHubPullRequest.ID != "" {
				branchesWithPRs = append(branchesWithPRs, branchName)
			}
		}

		if len(branchesWithPRs) > 0 {
			fmt.Printf("\nReminder: The following branches have PRs and were restacked:\n")

			for _, branchName := range branchesWithPRs {
				fmt.Printf("  - %s\n", branchName)
			}

			fmt.Printf("\nRun 'yas submit --outdated' to update the PRs with the rebased commits.\n")
		}
	}

	return nil
}

// findNearestLivingAncestor recursively walks up the parent chain in metadata
// until it finds a branch that exists in git, or reaches trunk.
func (yas *YAS) findNearestLivingAncestor(branchName string) (string, error) {
	// If we've reached trunk, that's the answer
	if branchName == yas.cfg.TrunkBranch {
		return yas.cfg.TrunkBranch, nil
	}

	// Check if this branch exists in metadata
	if !yas.data.Branches.Exists(branchName) {
		// Branch not in metadata, go to trunk
		return yas.cfg.TrunkBranch, nil
	}

	// Check if this branch exists as a git branch
	exists, err := yas.git.BranchExists(branchName)
	if err != nil {
		return "", err
	}

	if exists {
		// Found a living ancestor!
		return branchName, nil
	}

	// This branch doesn't exist, check its parent
	metadata := yas.data.Branches.Get(branchName)
	if metadata.Parent == "" {
		// No parent, go to trunk
		return yas.cfg.TrunkBranch, nil
	}

	// Recurse to the parent
	return yas.findNearestLivingAncestor(metadata.Parent)
}

// reparentIfParentDeleted checks if a branch's parent has been deleted and reparents
// it to the nearest living ancestor if so. Returns the effective parent to use for rebasing.
func (yas *YAS) reparentIfParentDeleted(branchName string) (string, error) {
	metadata := yas.data.Branches.Get(branchName)

	// If already pointing to trunk, nothing to do
	if metadata.Parent == "" || metadata.Parent == yas.cfg.TrunkBranch {
		return metadata.Parent, nil
	}

	// Check if parent exists as a git branch
	parentExists, err := yas.git.BranchExists(metadata.Parent)
	if err != nil {
		return "", err
	}

	if parentExists {
		// Parent exists, no reparenting needed
		return metadata.Parent, nil
	}

	// Parent was deleted, find the nearest living ancestor
	newParent, err := yas.findNearestLivingAncestor(metadata.Parent)
	if err != nil {
		return "", err
	}

	// Update metadata
	oldParent := metadata.Parent
	metadata.Parent = newParent
	yas.data.Branches.Set(branchName, metadata)

	// Save immediately to ensure partial progress is preserved
	if err := yas.data.Save(); err != nil {
		return "", fmt.Errorf("failed to save metadata after reparenting: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Warning: parent branch '%s' for '%s' no longer exists, reparenting to '%s'\n",
		oldParent, branchName, newParent)

	return newParent, nil
}

// buildRestackWorkQueue builds a queue of [child, parent] pairs representing
// the rebase operations that need to be performed.
func (yas *YAS) buildRestackWorkQueue(graph *dag.DAG, branchName string, workQueue *[][2]string) error {
	children, err := graph.GetChildren(branchName)
	if err != nil {
		return err
	}

	for childID := range children {
		// Check if this branch needs rebasing
		needsRebase, err := yas.needsRebase(childID, branchName)
		if err != nil {
			return err
		}

		if needsRebase {
			// Add to work queue
			*workQueue = append(*workQueue, [2]string{childID, branchName})
		}

		// Recursively add descendants to the work queue
		if err := yas.buildRestackWorkQueue(graph, childID, workQueue); err != nil {
			return err
		}
	}

	return nil
}

// processRestackWorkQueue processes a queue of rebase operations, saving state on error.
func (yas *YAS) processRestackWorkQueue(startingBranch string, workQueue [][2]string, rebasedBranches *[]string) error {
	for i, work := range workQueue {
		childBranch := work[0]
		parentBranch := work[1]

		// Get child metadata for branch point
		childMetadata := yas.data.Branches.Get(childBranch)

		childBranchExists, err := yas.git.BranchExists(childBranch)
		if err != nil {
			return err
		}

		if !childBranchExists {
			if childMetadata.Deleted == nil {
				now := time.Now()
				childMetadata.Deleted = &now
				yas.data.Branches.Set(childBranch, childMetadata)

				if err := yas.data.Save(); err != nil {
					return fmt.Errorf("failed to save metadata after marking branch as deleted: %w", err)
				}
			}

			continue
		}

		// Check if the parent has been deleted and reparent if necessary
		// This returns the effective parent to use for rebasing
		newParent, err := yas.reparentIfParentDeleted(childBranch)
		if err != nil {
			return err
		}

		// If reparenting occurred, skip the rebase
		if newParent != parentBranch {
			continue
		}

		// Perform the rebase
		if childMetadata.BranchPoint == "" {
			return fmt.Errorf("branch point is not set for %s", childBranch)
		}

		// Switch to the next branch in the work queue that we need to rebase
		branch, err := yas.git.WithBranchContext(childBranch)
		if err != nil {
			return err
		}

		rebaseErr := branch.RebaseOntoWithBranchPoint(parentBranch, childMetadata.BranchPoint, childBranch)
		if rebaseErr != nil {
			// Check if a rebase is actually in progress
			rebaseInProgress, err := branch.IsRebaseInProgress()
			if err != nil {
				return fmt.Errorf("rebase failed and unable to check rebase status: %w", err)
			}

			// If no rebase is in progress, this is a fatal error (e.g., unstashed changes)
			// Clean up any saved state and return the error
			if !rebaseInProgress {
				_ = yas.deleteRestackState()

				return fmt.Errorf("rebase failed for %s onto %s: %w", childBranch, parentBranch, rebaseErr)
			}

			// Rebase is in progress (e.g., conflicts), save state for resuming later
			if err := yas.saveRestackState(&RestackState{
				StartingBranch:  startingBranch,
				CurrentBranch:   childBranch,
				CurrentParent:   parentBranch,
				RemainingWork:   workQueue[i+1:],
				RebasedBranches: *rebasedBranches,
			}); err != nil {
				return fmt.Errorf("rebase failed and unable to save restack state: %w", err)
			}

			return fmt.Errorf("rebase failed for %s onto %s: %w\n\nFix conflicts and run 'yas continue' to resume", childBranch, parentBranch, rebaseErr)
		}

		// Update the branch point to the new parent commit
		parentCommit, err := yas.git.GetCommitHash(parentBranch)
		if err != nil {
			return fmt.Errorf("failed to get parent commit after rebase: %w", err)
		}

		childMetadata.BranchPoint = parentCommit
		yas.data.Branches.Set(childBranch, childMetadata)

		// Save updated metadata
		if err := yas.data.Save(); err != nil {
			return fmt.Errorf("failed to save metadata after rebase: %w", err)
		}

		// Track that this branch was rebased
		*rebasedBranches = append(*rebasedBranches, childBranch)
	}

	return nil
}

// Continue resumes a restack operation that was interrupted by conflicts.
func (yas *YAS) Continue() error {
	// Check if there's a saved restack state
	exists, err := yas.restackStateExists()
	if err != nil {
		return fmt.Errorf("failed to check restack state: %w", err)
	}

	if !exists {
		return errors.New("no restack operation in progress (no saved state found)")
	}

	// Load the saved state
	state, err := yas.loadRestackState()
	if err != nil {
		return fmt.Errorf("failed to load restack state: %w", err)
	}

	// Check if a rebase is currently in progress
	rebaseInProgress, err := yas.git.IsRebaseInProgress()
	if err != nil {
		return fmt.Errorf("failed to check if rebase is in progress: %w", err)
	}

	// If rebase is in progress, continue it until it completes
	if rebaseInProgress {
		fmt.Printf("Continuing rebase for %s...\n", state.CurrentBranch)

		for {
			if err := yas.git.RebaseContinue(); err != nil {
				return fmt.Errorf("rebase continue failed: %w\n\nFix conflicts and run 'yas continue' again", err)
			}

			// Check if rebase is still in progress
			stillInProgress, err := yas.git.IsRebaseInProgress()
			if err != nil {
				return fmt.Errorf("failed to check if rebase is in progress: %w", err)
			}

			if !stillInProgress {
				break
			}

			fmt.Printf("Continuing rebase...\n")
		}

		fmt.Printf("Rebase completed for %s\n", state.CurrentBranch)
	} else {
		// No rebase in progress - check if it was aborted or already completed
		// Verify the rebase actually happened by checking if the branch's merge-base with parent equals parent's current commit
		mergeBase, err := yas.git.GetMergeBase(state.CurrentBranch, state.CurrentParent)
		if err != nil {
			return fmt.Errorf("failed to get merge-base: %w", err)
		}

		parentCommit, err := yas.git.GetCommitHash(state.CurrentParent)
		if err != nil {
			return fmt.Errorf("failed to get parent commit: %w", err)
		}

		if mergeBase != parentCommit {
			// The rebase was likely aborted - the branch still diverges from parent
			return fmt.Errorf("rebase appears to have been aborted for %s (branch still diverges from parent)\n\nRun 'yas restack' to start over or manually rebase the branch", state.CurrentBranch)
		}

		fmt.Printf("Rebase already completed for %s\n", state.CurrentBranch)
	}

	// Update the branch point for the just-completed rebase
	childMetadata := yas.data.Branches.Get(state.CurrentBranch)

	parentCommit, err := yas.git.GetCommitHash(state.CurrentParent)
	if err != nil {
		return fmt.Errorf("failed to get parent commit after rebase: %w", err)
	}

	childMetadata.BranchPoint = parentCommit
	yas.data.Branches.Set(state.CurrentBranch, childMetadata)

	// Save updated metadata
	if err := yas.data.Save(); err != nil {
		return fmt.Errorf("failed to save metadata after rebase: %w", err)
	}

	// Track that this branch was rebased
	state.RebasedBranches = append(state.RebasedBranches, state.CurrentBranch)
	rebasedBranches := state.RebasedBranches

	// Keep rebuilding and processing until no more work remains
	// This loop is necessary because completing one rebase may cause descendant branches to need rebasing
	for {
		graph, err := yas.graph()
		if err != nil {
			return fmt.Errorf("failed to build graph: %w", err)
		}

		var newWorkQueue [][2]string
		if err := yas.buildRestackWorkQueue(graph, yas.cfg.TrunkBranch, &newWorkQueue); err != nil {
			return fmt.Errorf("failed to build work queue: %w", err)
		}

		// No more work to do
		if len(newWorkQueue) == 0 {
			break
		}

		fmt.Printf("\nContinuing restack with %d remaining branch(es)...\n", len(newWorkQueue))

		if err := yas.processRestackWorkQueue(state.StartingBranch, newWorkQueue, &rebasedBranches); err != nil {
			return err
		}
	}

	// Clean up the restack state file
	if err := yas.deleteRestackState(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to delete restack state: %v\n", err)
	}

	// Return to the starting branch
	if err := yas.git.QuietCheckout(state.StartingBranch); err != nil {
		return fmt.Errorf("restack succeeded but failed to return to branch %s: %w", state.StartingBranch, err)
	}

	// Check if any rebased branches have PRs
	if len(rebasedBranches) > 0 {
		branchesWithPRs := []string{}

		for _, branchName := range rebasedBranches {
			metadata := yas.data.Branches.Get(branchName)
			if metadata.GitHubPullRequest.ID != "" {
				branchesWithPRs = append(branchesWithPRs, branchName)
			}
		}

		if len(branchesWithPRs) > 0 {
			fmt.Printf("\nReminder: The following branches have PRs and were restacked:\n")

			for _, branchName := range branchesWithPRs {
				fmt.Printf("  - %s\n", branchName)
			}

			fmt.Printf("\nRun 'yas submit --outdated' to update the PRs with the rebased commits.\n")
		}
	}

	fmt.Printf("\nRestack completed successfully!\n")

	return nil
}

// Abort aborts an in-progress restack operation, resetting the current branch
// to its state before the rebase started.
func (yas *YAS) Abort() error {
	// Check if there's a saved restack state
	exists, err := yas.restackStateExists()
	if err != nil {
		return fmt.Errorf("failed to check restack state: %w", err)
	}

	if !exists {
		return errors.New("no restack operation in progress (no saved state found)")
	}

	// Load the saved state
	state, err := yas.loadRestackState()
	if err != nil {
		return fmt.Errorf("failed to load restack state: %w", err)
	}

	fmt.Printf("Aborting restack operation...\n")

	// Check if a rebase is currently in progress
	rebaseInProgress, err := yas.git.IsRebaseInProgress()
	if err != nil {
		return fmt.Errorf("failed to check if rebase is in progress: %w", err)
	}

	// If rebase is in progress, abort it
	if rebaseInProgress {
		fmt.Printf("Aborting in-progress rebase for %s...\n", state.CurrentBranch)

		if err := yas.git.RebaseAbort(); err != nil {
			return fmt.Errorf("failed to abort rebase: %w", err)
		}
	}

	// Delete the restack state
	if err := yas.deleteRestackState(); err != nil {
		return fmt.Errorf("failed to delete restack state: %w", err)
	}

	// Return to the starting branch
	if state.StartingBranch != "" && state.StartingBranch != state.CurrentBranch {
		fmt.Printf("Returning to %s...\n", state.StartingBranch)

		if err := yas.git.QuietCheckout(state.StartingBranch); err != nil {
			return fmt.Errorf("failed to return to starting branch %s: %w", state.StartingBranch, err)
		}
	}

	fmt.Printf("\nRestack aborted successfully.\n")

	if len(state.RebasedBranches) > 0 {
		fmt.Printf("Note: %d branch(es) were successfully rebased before the abort:\n", len(state.RebasedBranches))

		for _, branchName := range state.RebasedBranches {
			fmt.Printf("  - %s\n", branchName)
		}

		fmt.Printf("These branches remain in their rebased state.\n")
	}

	return nil
}

func (yas *YAS) needsRebase(branchName, parentBranch string) (bool, error) {
	// Get the branch metadata to access the stored branch point
	metadata := yas.data.Branches.Get(branchName)

	branchExists, err := yas.git.BranchExists(parentBranch)
	if err != nil {
		return false, err
	}

	// If the parent no longer exists, we need to rebase (onto trunk)
	if !branchExists {
		return true, nil
	}

	// Get the current commit of the parent branch
	parentCommit, err := yas.git.GetCommitHash(parentBranch)
	if err != nil {
		return false, err
	}

	// If branch point differs from parent's current commit, rebase is needed
	return metadata.BranchPoint != parentCommit, nil
}
