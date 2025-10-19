package yas

import (
	"errors"
	"fmt"
	"os"

	"github.com/heimdalr/dag"
)

// checkRestackInProgress returns an error if a restack operation is in progress.
func (yas *YAS) checkRestackInProgress() error {
	if RestackStateExists(yas.cfg.RepoDirectory) {
		return errors.New("a restack operation is already in progress\n\nRun 'yas continue' to resume or 'yas abort' to cancel")
	}

	return nil
}

// Restack rebases all branches starting from trunk, including all descendants
// and forks.
func (yas *YAS) Restack() error {
	// Check if a restack is already in progress
	if err := yas.checkRestackInProgress(); err != nil {
		return err
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
		if err := yas.buildRebaseWorkQueue(graph, yas.cfg.TrunkBranch, &workQueue); err != nil {
			return err
		}

		// No more work to do
		if len(workQueue) == 0 {
			break
		}

		// Process the work queue
		if err := yas.processRebaseWorkQueue(startingBranch, workQueue, &rebasedBranches); err != nil {
			return err
		}
	}

	// Clean up any restack state file (in case it exists)
	if err := DeleteRestackState(yas.cfg.RepoDirectory); err != nil {
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

			fmt.Printf("\nRun 'yas submit --stack' to update the PRs with the rebased commits.\n")
		}
	}

	return nil
}

// buildRebaseWorkQueue builds a queue of [child, parent] pairs representing
// the rebase operations that need to be performed.
func (yas *YAS) buildRebaseWorkQueue(graph *dag.DAG, branchName string, workQueue *[][2]string) error {
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
		if err := yas.buildRebaseWorkQueue(graph, childID, workQueue); err != nil {
			return err
		}
	}

	return nil
}

// processRebaseWorkQueue processes a queue of rebase operations, saving state on error.
func (yas *YAS) processRebaseWorkQueue(startingBranch string, workQueue [][2]string, rebasedBranches *[]string) error {
	for i, work := range workQueue {
		childBranch := work[0]
		parentBranch := work[1]

		// Get child metadata for branch point
		childMetadata := yas.data.Branches.Get(childBranch)

		// Perform the rebase
		if childMetadata.BranchPoint == "" {
			return fmt.Errorf("branch point is not set for %s", childBranch)
		}

		rebaseErr := yas.git.RebaseOntoWithBranchPoint(parentBranch, childMetadata.BranchPoint, childBranch)
		if rebaseErr != nil {
			// Check if a rebase is actually in progress
			rebaseInProgress, err := yas.git.IsRebaseInProgress()
			if err != nil {
				return fmt.Errorf("rebase failed and unable to check rebase status: %w", err)
			}

			// If no rebase is in progress, this is a fatal error (e.g., unstashed changes)
			// Don't save state, just return the error
			if !rebaseInProgress {
				return fmt.Errorf("rebase failed for %s onto %s: %w", childBranch, parentBranch, rebaseErr)
			}

			// Rebase is in progress (e.g., conflicts), save state for resuming later
			state := &RestackState{
				StartingBranch:  startingBranch,
				CurrentBranch:   childBranch,
				CurrentParent:   parentBranch,
				RemainingWork:   workQueue[i+1:],
				RebasedBranches: *rebasedBranches,
			}

			if err := SaveRestackState(yas.cfg.RepoDirectory, state); err != nil {
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
	if !RestackStateExists(yas.cfg.RepoDirectory) {
		return errors.New("no restack operation in progress (no saved state found)")
	}

	// Load the saved state
	state, err := LoadRestackState(yas.cfg.RepoDirectory)
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
		if err := yas.buildRebaseWorkQueue(graph, yas.cfg.TrunkBranch, &newWorkQueue); err != nil {
			return fmt.Errorf("failed to build work queue: %w", err)
		}

		// No more work to do
		if len(newWorkQueue) == 0 {
			break
		}

		fmt.Printf("\nContinuing restack with %d remaining branch(es)...\n", len(newWorkQueue))

		if err := yas.processRebaseWorkQueue(state.StartingBranch, newWorkQueue, &rebasedBranches); err != nil {
			return err
		}
	}

	// Clean up the restack state file
	if err := DeleteRestackState(yas.cfg.RepoDirectory); err != nil {
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

			fmt.Printf("\nRun 'yas submit --stack' to update the PRs with the rebased commits.\n")
		}
	}

	fmt.Printf("\nRestack completed successfully!\n")

	return nil
}

// Abort aborts an in-progress restack operation, resetting the current branch
// to its state before the rebase started.
func (yas *YAS) Abort() error {
	// Check if there's a saved restack state
	if !RestackStateExists(yas.cfg.RepoDirectory) {
		return errors.New("no restack operation in progress (no saved state found)")
	}

	// Load the saved state
	state, err := LoadRestackState(yas.cfg.RepoDirectory)
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
	if err := DeleteRestackState(yas.cfg.RepoDirectory); err != nil {
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

	// Get the current commit of the parent branch
	parentCommit, err := yas.git.GetCommitHash(parentBranch)
	if err != nil {
		return false, err
	}

	// If branch point differs from parent's current commit, rebase is needed
	return metadata.BranchPoint != parentCommit, nil
}
