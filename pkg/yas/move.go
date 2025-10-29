package yas

import (
	"errors"
	"fmt"
	"os"

	"github.com/heimdalr/dag"
)

// Move rebases the current branch and all its descendants onto a new parent branch.
func (yas *YAS) Move(targetBranch string) error {
	return yas.MoveBranch("", targetBranch)
}

// MoveBranch rebases the specified branch and all its descendants onto a new parent branch.
func (yas *YAS) MoveBranch(branchName, targetBranch string) error {
	// Check if a restack is already in progress
	if err := yas.checkRestackInProgress(); err != nil {
		return err
	}

	// Get the branch to move (default to current branch if not specified)
	var (
		currentBranch string
		err           error
	)

	if branchName == "" {
		currentBranch, err = yas.git.GetCurrentBranchName()
		if err != nil {
			return err
		}
	} else {
		currentBranch = branchName
		// Switch to the specified branch
		if err := yas.git.QuietCheckout(branchName); err != nil {
			return fmt.Errorf("failed to checkout branch %s: %w", branchName, err)
		}
	}

	// Can't move trunk branch
	if currentBranch == yas.cfg.TrunkBranch {
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

	// Get current branch metadata
	currentMetadata := yas.data.Branches.Get(currentBranch)
	if currentMetadata.BranchPoint == "" {
		return fmt.Errorf("branch point is not set for %s (run 'yas add' first)", currentBranch)
	}

	// Build a graph to get descendants
	graph, err := yas.graph()
	if err != nil {
		return fmt.Errorf("failed to build graph: %w", err)
	}

	// Get all descendants of the current branch
	var descendants []string
	if err := yas.collectDescendants(graph, currentBranch, &descendants); err != nil {
		return fmt.Errorf("failed to collect descendants: %w", err)
	}

	// Build work queue: current branch + all descendants
	var workQueue [][2]string

	// First, rebase current branch onto target
	workQueue = append(workQueue, [2]string{currentBranch, targetBranch})

	// Then, add all descendants to the work queue (they ALL need to be rebased
	// because their parent has moved, even if their branch point hasn't changed)
	// We need to add them in the correct order: parents before children
	if err := yas.buildMoveWorkQueue(graph, currentBranch, &workQueue); err != nil {
		return fmt.Errorf("failed to build work queue for descendants: %w", err)
	}

	fmt.Printf("Moving %s and %d descendant(s) onto %s...\n", currentBranch, len(descendants), targetBranch)

	// Update the current branch's parent metadata BEFORE rebasing
	// This is needed so that when descendants are rebased, they see the correct parent
	currentMetadata.Parent = targetBranch
	yas.data.Branches.Set(currentBranch, currentMetadata)

	// Save updated metadata before rebasing
	if err := yas.data.Save(); err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	// Track which branches were rebased
	rebasedBranches := []string{}

	// Process the work queue
	if err := yas.processRestackWorkQueue(currentBranch, workQueue, &rebasedBranches); err != nil {
		return err
	}

	// Clean up any restack state file (in case it exists)
	if err := DeleteRestackState(yas.cfg.RepoDirectory); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to delete restack state: %v\n", err)
	}

	// Return to the current branch (we should already be on it)
	if err := yas.git.QuietCheckout(currentBranch); err != nil {
		return fmt.Errorf("move succeeded but failed to return to branch %s: %w", currentBranch, err)
	}

	fmt.Printf("\nMove completed successfully!\n")

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
			fmt.Printf("\nReminder: The following branches have PRs and were moved:\n")

			for _, branchName := range branchesWithPRs {
				fmt.Printf("  - %s\n", branchName)
			}

			fmt.Printf("\nRun 'yas submit --stack' to update the PRs with the rebased commits.\n")
		}
	}

	return nil
}

// buildMoveWorkQueue builds a queue for moving a subtree - it includes ALL descendants
// regardless of whether they "need" rebasing, because we're moving the entire subtree.
func (yas *YAS) buildMoveWorkQueue(graph *dag.DAG, branchName string, workQueue *[][2]string) error {
	children, err := graph.GetChildren(branchName)
	if err != nil {
		return err
	}

	for childID := range children {
		// Always add to work queue (unlike buildRebaseWorkQueue which checks needsRebase)
		*workQueue = append(*workQueue, [2]string{childID, branchName})

		// Recursively add descendants to the work queue
		if err := yas.buildMoveWorkQueue(graph, childID, workQueue); err != nil {
			return err
		}
	}

	return nil
}
