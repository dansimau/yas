package yas

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dansimau/yas/pkg/log"
	"github.com/dansimau/yas/pkg/progress"
	"github.com/dansimau/yas/pkg/xexec"
)

func (yas *YAS) Submit(draft bool) error {
	// Check if a restack is in progress (do this before getting branch name
	// which would fail in detached HEAD state during rebase)
	if err := yas.errIfRestackInProgress(); err != nil {
		return err
	}

	currentBranch, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return err
	}

	if currentBranch == "HEAD" {
		return errors.New("cannot submit in detached HEAD state")
	}

	if currentBranch == yas.cfg.TrunkBranch {
		return errors.New("cannot submit trunk branch")
	}

	// Submit and annotate the current branch
	return yas.submitBranches([]string{currentBranch}, draft)
}

func (yas *YAS) SubmitOutdated(draft bool) error {
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

	// Submit and annotate all branches that need updating
	return yas.submitBranches(branchesToSubmit, draft)
}

func (yas *YAS) SubmitStack(draft bool) error {
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

	// Extract the current stack from the full graph
	stackGraph, err := yas.currentStackGraph(fullGraph, currentBranch)
	if err != nil {
		return fmt.Errorf("failed to get current stack: %w", err)
	}

	var stackBranches []string

	// Walk the stack in topological order
	stackGraph.OrderedWalk(&branchVisitor{
		branches:    &stackBranches,
		trunkBranch: yas.cfg.TrunkBranch,
	})

	// Submit and annotate all branches in the stack
	return yas.submitBranches(stackBranches, draft)
}

// submitBranches submits multiple branches in parallel and then annotates them in parallel.
func (yas *YAS) submitBranches(branches []string, draft bool) error {
	// Check if a restack is in progress (do this before getting branch name
	// which would fail in detached HEAD state during rebase)
	if err := yas.errIfRestackInProgress(); err != nil {
		return err
	}

	if len(branches) == 0 {
		return nil
	}

	// Phase 1: Submit all branches in parallel
	submitRunner := progress.New(5, "Submitting branches:")

	for _, branchName := range branches {
		submitRunner.Add(branchName, func() error {
			return yas.submitBranch(branchName, draft, func(s string) {
				submitRunner.UpdateStatusLine(branchName, s)
			})
		})
	}

	if err := submitRunner.Start(true); err != nil {
		return err
	}

	// Phase 2: Annotate all branches in parallel
	annotateRunner := progress.New(5, "\nAnnotating PRs:")

	for _, branchName := range branches {
		annotateRunner.Add(branchName, func() error {
			return yas.annotateBranch(branchName)
		})
	}

	if err := annotateRunner.Start(true); err != nil {
		return err
	}

	fmt.Printf("\nSuccessfully submitted and annotated %d branch(es)\n", len(branches))

	return nil
}

func (yas *YAS) submitBranch(branchName string, draft bool, status func(string)) error {
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

		status("Pushing branch...")
		// Force push with lease (we expect the branch may have been rebased)
		if err := yas.git.ForcePushBranch(remote, branchName); err != nil {
			return fmt.Errorf("failed to push: %w", err)
		}
	}

	// Check if PR already exists
	if metadata.GitHubPullRequest.ID != "" {
		// Check if base branch needs updating
		needsBaseUpdate := metadata.Parent != "" &&
			metadata.GitHubPullRequest.BaseRefName != "" &&
			metadata.Parent != metadata.GitHubPullRequest.BaseRefName

		if needsBaseUpdate {
			prNumber := extractPRNumber(metadata.GitHubPullRequest.URL)

			status("Updating PR base branch...")

			if err := xexec.Command("gh", "pr", "edit", prNumber, "--base", metadata.Parent).WithStdout(nil).Run(); err != nil {
				return fmt.Errorf("failed to update PR base branch: %w", err)
			}

			// Refresh remote status to update our cached base branch
			if err := yas.refreshRemoteStatus(branchName); err != nil {
				return err
			}

			// Update metadata after refresh
			metadata = yas.data.Branches.Get(branchName)
		}

		state := yas.prStateToYasState(metadata.GitHubPullRequest)

		// Show appropriate message based on what happened
		switch {
		case !needsPush && !needsBaseUpdate:
			status(fmt.Sprintf("%s (state: %s), up to date",
				metadata.GitHubPullRequest.URL,
				state))
		case oldRemoteHash != "" && oldRemoteHash != currentLocalHash:
			status(fmt.Sprintf("%s (state: %s), force pushed (was: %s)",
				metadata.GitHubPullRequest.URL,
				state,
				oldRemoteHash))
		default:
			status(fmt.Sprintf("%s (state: %s), pushed new commits",
				metadata.GitHubPullRequest.URL,
				state))
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

	if err := xexec.Command(append([]string{"gh", "pr", "create"}, prCreateArgs...)...).WithStdout(nil).Run(); err != nil {
		return err
	}

	// Refresh the remote status to get the new PR metadata
	if err := yas.refreshRemoteStatus(branchName); err != nil {
		return err
	}

	metadata = yas.data.Branches.Get(branchName)
	status(fmt.Sprintf("%s (state: %s), created",
		metadata.GitHubPullRequest.URL,
		yas.prStateToYasState(metadata.GitHubPullRequest)))

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
