package yas

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dansimau/yas/pkg/log"
	"github.com/dansimau/yas/pkg/progress"
	"github.com/dansimau/yas/pkg/xexec"
	"github.com/heimdalr/dag"
)

func (yas *YAS) Submit(draft bool) error {
	// Check if a restack is in progress (do this before getting branch name
	// which would fail in detached HEAD state during rebase)
	if err := yas.checkRestackInProgress(); err != nil {
		return err
	}

	currentBranch, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return err
	}

	if currentBranch == "HEAD" {
		return errors.New("cannot submit in detached HEAD state")
	}

	if err := yas.submitBranch(currentBranch, draft); err != nil {
		return err
	}

	// Annotate the PR with stack information
	if err := yas.annotateBranch(currentBranch); err != nil {
		// Don't fail the submit if annotation fails
		fmt.Printf("Warning: failed to annotate PR: %v\n", err)
	}

	return nil
}

func (yas *YAS) SubmitOutdated(draft bool) error {
	// Check if a restack is in progress (do this before getting branch name
	// which would fail in detached HEAD state during rebase)
	if err := yas.checkRestackInProgress(); err != nil {
		return err
	}

	// Get all branches with PRs (optimization: skip branches without PRs)
	branchesWithPRs := yas.data.Branches.ToSlice().WithPRs()

	if len(branchesWithPRs) == 0 {
		fmt.Println("No branches with PRs found")

		return nil
	}

	// Find branches that need submitting
	var branchesToSubmit []string

	for _, branch := range branchesWithPRs {
		needsSubmit, err := yas.needsSubmit(branch.Name)
		if err != nil {
			return fmt.Errorf("failed to check if %s needs submit: %w", branch.Name, err)
		}

		if needsSubmit {
			branchesToSubmit = append(branchesToSubmit, branch.Name)
		}
	}

	if len(branchesToSubmit) == 0 {
		fmt.Println("No branches need submitting")

		return nil
	}

	fmt.Printf("Found %d branch(es) that need submitting:\n", len(branchesToSubmit))

	for _, branchName := range branchesToSubmit {
		fmt.Printf("  - %s\n", branchName)
	}

	// Submit each branch that needs updating
	var submittedBranches []string

	for _, branchName := range branchesToSubmit {
		fmt.Printf("\n=== Submitting %s ===\n", branchName)

		if err := yas.submitBranch(branchName, draft); err != nil {
			return fmt.Errorf("failed to submit %s: %w", branchName, err)
		}

		submittedBranches = append(submittedBranches, branchName)
	}

	// Annotate all submitted branches with stack information
	runner := progress.New(5, "\nAnnotating PRs:")
	for _, branchName := range submittedBranches {
		runner.Add(branchName, func() error {
			return yas.annotateBranch(branchName)
		})
	}

	if err := runner.Start(true); err != nil {
		return err
	}

	fmt.Printf("\nSuccessfully submitted %d outdated branch(es)\n", len(submittedBranches))

	return nil
}

func (yas *YAS) SubmitStack(draft bool) error {
	// Check if a restack is in progress (do this before getting branch name
	// which would fail in detached HEAD state during rebase)
	if err := yas.checkRestackInProgress(); err != nil {
		return err
	}

	currentBranch, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return err
	}

	if currentBranch == "HEAD" {
		return errors.New("cannot submit in detached HEAD state")
	}

	// Get the full graph
	fullGraph, err := yas.graph()
	if err != nil {
		return fmt.Errorf("failed to get graph: %w", err)
	}

	// Get the current stack
	stackGraph, err := yas.currentStackGraph(fullGraph, currentBranch)
	if err != nil {
		return fmt.Errorf("failed to get current stack: %w", err)
	}

	// First pass: Submit all branches in the stack starting from trunk
	var submittedBranches []string
	if err := yas.submitDescendants(stackGraph, yas.cfg.TrunkBranch, &submittedBranches, draft); err != nil {
		return err
	}

	// Second pass: Annotate all submitted branches now that all PRs exist
	runner := progress.New(5, "\nAnnotating PRs:")

	for _, branchName := range submittedBranches {
		runner.Add(branchName, func() error {
			return yas.annotateBranch(branchName)
		})
	}

	if err := runner.Start(true); err != nil {
		return err
	}

	fmt.Printf("\nSuccessfully submitted all branches in stack\n")

	return nil
}

func (yas *YAS) submitDescendants(graph *dag.DAG, branchName string, submittedBranches *[]string, draft bool) error {
	children, err := graph.GetChildren(branchName)
	if err != nil {
		return err
	}

	for childID := range children {
		fmt.Printf("\n=== Submitting %s ===\n", childID)

		if err := yas.submitBranch(childID, draft); err != nil {
			return fmt.Errorf("failed to submit %s: %w", childID, err)
		}

		// Track that we submitted this branch
		*submittedBranches = append(*submittedBranches, childID)

		// Recursively submit this branch's descendants
		if err := yas.submitDescendants(graph, childID, submittedBranches, draft); err != nil {
			return err
		}
	}

	return nil
}

func (yas *YAS) submitBranch(branchName string, draft bool) error {
	if err := yas.refreshRemoteStatus(branchName); err != nil {
		return err
	}

	metadata := yas.data.Branches.Get(branchName)

	// Get current local hash
	currentLocalHash, err := yas.git.GetShortHash(branchName)
	if err != nil {
		return fmt.Errorf("failed to get local hash: %w", err)
	}

	// Get old remote hash before pushing (for showing in output)
	var (
		oldRemoteHash string
		remoteExists  bool
	)

	if metadata.GitHubPullRequest.ID != "" {
		// Fetch the latest remote ref
		if err := yas.git.FetchBranch(branchName); err != nil {
			// Ignore error if remote branch doesn't exist yet
			log.Info("Failed to fetch remote branch (may not exist yet)", err)
		} else {
			// Get the short hash of the remote branch before pushing
			hash, err := yas.git.GetRemoteShortHash(branchName)
			if err == nil {
				oldRemoteHash = hash
				remoteExists = true
			}
		}
	}

	// Check if we need to push
	needsPush := !remoteExists || oldRemoteHash != currentLocalHash

	if needsPush {
		// Get remote for this branch, or trunk if no remote is configured
		remote, err := yas.git.GetRemoteForBranch(branchName, yas.cfg.TrunkBranch)
		if err != nil {
			return fmt.Errorf("failed to get remote for branch %s or trunk: %w", branchName, err)
		}

		// Force push with lease (we expect the branch may have been rebased)
		if err := yas.git.ForcePushBranch(remote, branchName); err != nil {
			return fmt.Errorf("failed to push: %w", err)
		}
	}

	// Check if PR already exists
	if metadata.GitHubPullRequest.ID != "" {
		state := metadata.GitHubPullRequest.State
		if metadata.GitHubPullRequest.IsDraft {
			state = "DRAFT"
		}

		// Check if base branch needs updating
		needsBaseUpdate := metadata.Parent != "" &&
			metadata.GitHubPullRequest.BaseRefName != "" &&
			metadata.Parent != metadata.GitHubPullRequest.BaseRefName

		if needsBaseUpdate {
			prNumber := extractPRNumber(metadata.GitHubPullRequest.URL)
			fmt.Printf("Updating PR base branch from %s to %s...\n",
				metadata.GitHubPullRequest.BaseRefName,
				metadata.Parent)

			if err := xexec.Command("gh", "pr", "edit", prNumber, "--base", metadata.Parent).Run(); err != nil {
				return fmt.Errorf("failed to update PR base branch: %w", err)
			}

			// Refresh remote status to update our cached base branch
			if err := yas.refreshRemoteStatus(branchName); err != nil {
				return err
			}

			// Update metadata after refresh
			metadata = yas.data.Branches.Get(branchName)
		}

		// Show appropriate message based on what happened
		switch {
		case !needsPush && !needsBaseUpdate:
			fmt.Printf("PR exists: %s (state: %s), up to date\n",
				metadata.GitHubPullRequest.URL,
				state)
		case oldRemoteHash != "" && oldRemoteHash != currentLocalHash:
			fmt.Printf("PR exists: %s (state: %s), force pushed (was: %s)\n",
				metadata.GitHubPullRequest.URL,
				state,
				oldRemoteHash)
		default:
			fmt.Printf("PR exists: %s (state: %s), pushed new commits\n",
				metadata.GitHubPullRequest.URL,
				state)
		}

		return nil
	}

	// Create new PR
	prCreateArgs := []string{
		"--fill-first",
		"--head", branchName,
	}

	if draft {
		prCreateArgs = append(prCreateArgs, "--draft")
	}

	if metadata.Parent != "" {
		prCreateArgs = append(prCreateArgs, "--base", metadata.Parent)
	}

	if err := xexec.Command(append([]string{"gh", "pr", "create"}, prCreateArgs...)...).Run(); err != nil {
		return err
	}

	// Refresh the remote status to get the new PR metadata
	if err := yas.refreshRemoteStatus(branchName); err != nil {
		return err
	}

	return nil
}

// needsSubmit checks if a branch needs to be submitted
// A branch needs submitting when:
// 1. Local commit doesn't match remote commit, OR
// 2. Local parent doesn't match PR base branch.
func (yas *YAS) needsSubmit(branchName string) (bool, error) {
	metadata := yas.data.Branches.Get(branchName)

	// If no PR exists, doesn't need submit (needs creation instead)
	if metadata.GitHubPullRequest.ID == "" {
		return false, nil
	}

	// Check if local commit matches remote
	localHash, err := yas.git.GetCommitHash(branchName)
	if err != nil {
		return false, err
	}

	// Try to get remote hash
	remoteHash, err := yas.git.GetRemoteCommitHash(branchName)
	if err != nil {
		// If we can't get remote hash, assume it needs submit
		return true, nil
	}

	// If commits differ, needs submit
	if localHash != remoteHash {
		return true, nil
	}

	// Check if parent matches PR base
	if metadata.Parent != "" && metadata.GitHubPullRequest.BaseRefName != "" {
		if metadata.Parent != metadata.GitHubPullRequest.BaseRefName {
			return true, nil
		}
	}

	return false, nil
}

func extractPRNumber(prURL string) string {
	parts := strings.Split(prURL, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return ""
}
