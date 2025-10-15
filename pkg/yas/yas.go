// Package yas provides the core business logic for the yas tool.
package yas

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/dansimau/yas/pkg/log"
	"github.com/dansimau/yas/pkg/xexec"
	"github.com/fatih/color"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/hashicorp/go-version"
	"github.com/heimdalr/dag"
	"github.com/sourcegraph/conc/pool"
)

var minimumRequiredGitVersion = version.Must(version.NewVersion("2.38"))

const (
	yasStateFile     = ".git/.yasstate"
	restackStateFile = ".git/.yasrestack"
)

type YAS struct {
	cfg  Config
	data *yasDatabase
	git  *gitexec.Repo
	repo *git.Repository
}

func New(cfg Config) (*YAS, error) {
	repo, err := git.PlainOpen(cfg.RepoDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to open git repo: %w", err)
	}

	data, err := loadData(path.Join(cfg.RepoDirectory, yasStateFile))
	if err != nil {
		return nil, fmt.Errorf("failed to load YAS state: %w", err)
	}

	yas := &YAS{
		cfg:  cfg,
		data: data,
		git:  gitexec.WithRepo(cfg.RepoDirectory),
		repo: repo,
	}

	if err := yas.validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	if err := yas.pruneMissingBranches(); err != nil {
		return nil, fmt.Errorf("failed to prune missing branches: %w", err)
	}

	return yas, nil
}

func NewFromRepository(repoDirectory string) (*YAS, error) {
	cfg, err := ReadConfig(repoDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	return New(*cfg)
}

func (yas *YAS) cleanupBranch(name string) error {
	yas.data.Branches.Remove(name)

	return yas.data.Save()
}

func (yas *YAS) pruneMissingBranches() error {
	removed := false

	for _, branch := range yas.data.Branches.ToSlice() {
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

func (yas *YAS) Config() Config {
	return yas.cfg
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

func (yas *YAS) fetchGitHubPullRequestStatus(branchName string) (*PullRequestMetadata, error) {
	log.Info("Fetching PRs for branch", branchName)

	b, err := xexec.Command("gh", "pr", "list", "--head", branchName, "--state", "all", "--json", "id,state,url,isDraft,baseRefName").WithStdout(nil).Output()
	if err != nil {
		return nil, err
	}

	data := []PullRequestMetadata{}
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, nil
	}

	return &data[0], nil
}

// fetchPRStatusWithChecks fetches PR status including review decision and CI checks.
func (yas *YAS) fetchPRStatusWithChecks(branchName string) (*PullRequestMetadata, error) {
	log.Info("Fetching PR status with checks for branch", branchName)

	b, err := xexec.Command("gh", "pr", "list", "--head", branchName, "--state", "all", "--json", "id,state,url,isDraft,baseRefName,reviewDecision,statusCheckRollup").WithStdout(nil).Output()
	if err != nil {
		return nil, err
	}

	data := []PullRequestMetadata{}
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, nil
	}

	return &data[0], nil
}

func (yas *YAS) graph() (*dag.DAG, error) {
	graph := dag.NewDAG()

	// Use branch name string as vertex value (must be hashable and unique)
	if err := graph.AddVertexByID(yas.cfg.TrunkBranch, yas.cfg.TrunkBranch); err != nil {
		return nil, err
	}

	for _, branch := range yas.data.Branches.ToSlice().WithParents() {
		if err := graph.AddVertexByID(branch.Name, branch.Name); err != nil {
			return nil, err
		}
	}

	for _, branch := range yas.data.Branches.ToSlice().WithParents() {
		if err := graph.AddEdge(branch.Parent, branch.Name); err != nil {
			return nil, err
		}
	}

	return graph, nil
}

// currentStackGraph returns a subgraph containing only the current stack:
// - Upwards: only parents in the current lineage to the trunk branch
// - Downwards: all descendants, including those with multiple children.
func (yas *YAS) currentStackGraph(fullGraph *dag.DAG, currentBranch string) (*dag.DAG, error) {
	stackGraph := dag.NewDAG()

	// If current branch is trunk, return full graph
	if currentBranch == yas.cfg.TrunkBranch {
		return fullGraph, nil
	}

	// Get all ancestors (single lineage upwards to trunk)
	ancestors, err := fullGraph.GetAncestors(currentBranch)
	if err != nil {
		return nil, err
	}

	// Get all descendants (all child lineages)
	descendants, err := fullGraph.GetDescendants(currentBranch)
	if err != nil {
		return nil, err
	}

	// Collect all vertices in the current stack (ancestors + current + descendants)
	stackVertices := make(map[string]bool)
	for id := range ancestors {
		stackVertices[id] = true
	}

	stackVertices[currentBranch] = true
	for id := range descendants {
		stackVertices[id] = true
	}

	// Add vertices to the new graph
	for id := range stackVertices {
		vertex, err := fullGraph.GetVertex(id)
		if err != nil {
			return nil, err
		}

		if err := stackGraph.AddVertexByID(id, vertex); err != nil {
			return nil, err
		}
	}

	// Add edges between vertices that are both in the stack
	for id := range stackVertices {
		children, err := fullGraph.GetChildren(id)
		if err != nil {
			return nil, err
		}

		for childID := range children {
			if stackVertices[childID] {
				if err := stackGraph.AddEdge(id, childID); err != nil {
					return nil, err
				}
			}
		}
	}

	return stackGraph, nil
}

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
	if err := yas.processRebaseWorkQueue(currentBranch, workQueue, &rebasedBranches); err != nil {
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

func (yas *YAS) List(currentStackOnly bool, showStatus bool) error {
	items, err := yas.GetBranchList(currentStackOnly, showStatus)
	if err != nil {
		return err
	}

	for _, item := range items {
		fmt.Println(item.Line)
	}

	return nil
}

// GetBranchList returns a list of SelectionItems representing all branches
// Each item contains the branch name (ID) and the formatted display line.
func (yas *YAS) GetBranchList(currentStackOnly bool, showStatus bool) ([]SelectionItem, error) {
	graph, err := yas.graph()
	if err != nil {
		return nil, fmt.Errorf("failed to get graph: %w", err)
	}

	currentBranch, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return nil, err
	}

	// If status flag is set, fetch PR status for all branches with PRs
	if showStatus {
		branchesWithPRs := yas.data.Branches.ToSlice().WithPRs()
		if len(branchesWithPRs) > 0 {
			if err := yas.RefreshPRStatus(branchesWithPRs.BranchNames()...); err != nil {
				return nil, fmt.Errorf("failed to refresh PR status: %w", err)
			}
		}
	}

	// Filter to current stack if requested
	if currentStackOnly {
		graph, err = yas.currentStackGraph(graph, currentBranch)
		if err != nil {
			return nil, fmt.Errorf("failed to get current stack: %w", err)
		}
	}

	// Build list of SelectionItems
	var items []SelectionItem

	// Add trunk branch first
	rootLabel := formatBranchName(yas.cfg.TrunkBranch)
	if yas.cfg.TrunkBranch == currentBranch {
		darkGray := color.New(color.FgHiBlack).SprintFunc()
		rootLabel = fmt.Sprintf("%s %s", rootLabel, darkGray("*"))
	}

	items = append(items, SelectionItem{
		ID:   yas.cfg.TrunkBranch,
		Line: rootLabel,
	})

	// Collect child branches recursively
	if err := yas.collectBranchItems(&items, graph, yas.cfg.TrunkBranch, currentBranch, showStatus, ""); err != nil {
		return nil, err
	}

	return items, nil
}

// collectBranchItems recursively collects branch items with tree formatting.
func (yas *YAS) collectBranchItems(items *[]SelectionItem, graph *dag.DAG, parentID string, currentBranch string, showStatus bool, prefix string) error {
	children, err := graph.GetChildren(parentID)
	if err != nil {
		return err
	}

	// Convert to slice for ordering
	childIDs := make([]string, 0, len(children))
	for childID := range children {
		childIDs = append(childIDs, childID)
	}

	for i, childID := range childIDs {
		isLastChild := i == len(childIDs)-1

		// Build branch label (same as addNodesFromGraph)
		branchLabel := formatBranchName(childID)
		branchMetadata := yas.data.Branches.Get(childID)

		// Check if this branch needs rebasing or submitting
		var statusParts []string

		yellow := color.New(color.FgYellow).SprintFunc()

		needsRebase, err := yas.needsRebase(childID, parentID)
		if err == nil && needsRebase {
			statusParts = append(statusParts, "needs restack")
		}

		// Check submit status
		if branchMetadata.GitHubPullRequest.ID == "" {
			statusParts = append(statusParts, "not submitted")
		} else {
			needsSubmit, err := yas.needsSubmit(childID)
			if err == nil && needsSubmit {
				statusParts = append(statusParts, "needs submit")
			}
		}

		// Add combined status if any
		if len(statusParts) > 0 {
			branchLabel = fmt.Sprintf("%s %s", branchLabel, yellow(fmt.Sprintf("(%s)", strings.Join(statusParts, ", "))))
		}

		// Add PR information if available
		if branchMetadata.GitHubPullRequest.ID != "" {
			pr := branchMetadata.GitHubPullRequest
			cyan := color.New(color.FgCyan).SprintFunc()
			branchLabel = fmt.Sprintf("%s %s", branchLabel, cyan(fmt.Sprintf("[%s]", pr.URL)))

			// Add review and CI status if requested
			if showStatus {
				reviewStatus := getReviewStatusIcon(pr.ReviewDecision)
				ciStatus := getCIStatusIcon(pr.GetOverallCIStatus())
				darkGray := color.New(color.FgHiBlack).SprintFunc()
				branchLabel = fmt.Sprintf("%s %s", branchLabel, darkGray(fmt.Sprintf("(review: %s, CI: %s)", reviewStatus, ciStatus)))
			}
		}

		// Add star if this is the current branch
		if childID == currentBranch {
			darkGray := color.New(color.FgHiBlack).SprintFunc()
			branchLabel = fmt.Sprintf("%s %s", branchLabel, darkGray("*"))
		}

		// Build tree prefix
		var treeChar string
		if isLastChild {
			treeChar = "└── "
		} else {
			treeChar = "├── "
		}

		line := prefix + treeChar + branchLabel

		*items = append(*items, SelectionItem{
			ID:   childID,
			Line: line,
		})

		// Recurse for children
		var newPrefix string
		if isLastChild {
			newPrefix = prefix + "    "
		} else {
			newPrefix = prefix + "│   "
		}

		if err := yas.collectBranchItems(items, graph, childID, currentBranch, showStatus, newPrefix); err != nil {
			return err
		}
	}

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

func (yas *YAS) SetParent(branchName, parentBranchName, branchPoint string) error {
	if branchName == "" {
		currentBranch, err := yas.git.GetCurrentBranchName()
		if err != nil {
			return err
		}

		branchName = currentBranch
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

	yas.data.Branches.Set(branchName, branchMetdata)

	if err := yas.data.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to save data: %v\n", err)

		return fmt.Errorf("failed to save data: %w", err)
	}

	fmt.Printf("Set '%s' as parent of '%s'\n", parentBranchName, branchName)

	return nil
}

func (yas *YAS) Submit() error {
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

	if err := yas.submitBranch(currentBranch); err != nil {
		return err
	}

	// Annotate the PR with stack information
	if err := yas.annotateBranch(currentBranch); err != nil {
		// Don't fail the submit if annotation fails
		fmt.Printf("Warning: failed to annotate PR: %v\n", err)
	}

	return nil
}

func (yas *YAS) SubmitOutdated() error {
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

		if err := yas.submitBranch(branchName); err != nil {
			return fmt.Errorf("failed to submit %s: %w", branchName, err)
		}

		submittedBranches = append(submittedBranches, branchName)
	}

	// Annotate all submitted branches with stack information
	fmt.Printf("\n=== Annotating PRs ===\n")

	for _, branchName := range submittedBranches {
		if err := yas.annotateBranch(branchName); err != nil {
			// Don't fail the submit if annotation fails
			fmt.Printf("Warning: failed to annotate PR for %s: %v\n", branchName, err)
		}
	}

	fmt.Printf("\nSuccessfully submitted %d outdated branch(es)\n", len(submittedBranches))

	return nil
}

func (yas *YAS) SubmitStack() error {
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
	if err := yas.submitDescendants(stackGraph, yas.cfg.TrunkBranch, &submittedBranches); err != nil {
		return err
	}

	// Second pass: Annotate all submitted branches now that all PRs exist
	fmt.Printf("\n=== Annotating PRs ===\n")

	for _, branch := range submittedBranches {
		if err := yas.annotateBranch(branch); err != nil {
			// Don't fail the submit if annotation fails
			fmt.Printf("Warning: failed to annotate PR for %s: %v\n", branch, err)
		}
	}

	fmt.Printf("\nSuccessfully submitted all branches in stack\n")

	return nil
}

func (yas *YAS) submitDescendants(graph *dag.DAG, branchName string, submittedBranches *[]string) error {
	children, err := graph.GetChildren(branchName)
	if err != nil {
		return err
	}

	for childID := range children {
		fmt.Printf("\n=== Submitting %s ===\n", childID)

		if err := yas.submitBranch(childID); err != nil {
			return fmt.Errorf("failed to submit %s: %w", childID, err)
		}

		// Track that we submitted this branch
		*submittedBranches = append(*submittedBranches, childID)

		// Recursively submit this branch's descendants
		if err := yas.submitDescendants(graph, childID, submittedBranches); err != nil {
			return err
		}
	}

	return nil
}

func (yas *YAS) submitBranch(branchName string) error {
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
		// Force push with lease (we expect the branch may have been rebased)
		if err := yas.git.ForcePushBranch(branchName); err != nil {
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
		"--draft",
		"--fill-first",
		"--head", branchName,
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

func (yas *YAS) Annotate() error {
	currentBranch, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return err
	}

	if currentBranch == "HEAD" {
		return errors.New("cannot annotate in detached HEAD state")
	}

	return yas.annotateBranch(currentBranch)
}

func (yas *YAS) annotateBranch(branchName string) error {
	// Get the branch metadata
	metadata := yas.data.Branches.Get(branchName)
	if metadata.GitHubPullRequest.ID == "" {
		return fmt.Errorf("branch '%s' does not have a PR", branchName)
	}

	// Count PRs in the stack
	prCount, err := yas.countPRsInStack(branchName)
	if err != nil {
		return fmt.Errorf("failed to count PRs in stack: %w", err)
	}

	// Get the current PR body
	prNumber := extractPRNumber(metadata.GitHubPullRequest.URL)

	currentBody, err := yas.getPRBody(prNumber)
	if err != nil {
		return fmt.Errorf("failed to get PR body: %w", err)
	}

	var newBody string
	if prCount <= 1 {
		// Remove stack section if it exists
		newBody = removeStackSection(currentBody)
	} else {
		// Build the stack visualization
		stackVisualization, err := yas.buildStackVisualization(branchName)
		if err != nil {
			return fmt.Errorf("failed to build stack visualization: %w", err)
		}
		// Update the body with the stack section
		newBody = updatePRBodyWithStack(currentBody, stackVisualization)
	}

	// Update the PR
	if err := yas.updatePRBody(prNumber, newBody); err != nil {
		return fmt.Errorf("failed to update PR: %w", err)
	}

	fmt.Printf("Updated PR #%s with stack information\n", prNumber)

	return nil
}

func (yas *YAS) countPRsInStack(currentBranch string) (int, error) {
	// Get the graph
	graph, err := yas.graph()
	if err != nil {
		return 0, err
	}

	count := 0

	// Count ancestors (walking up to trunk)
	branch := currentBranch
	for {
		metadata := yas.data.Branches.Get(branch)
		if metadata.GitHubPullRequest.ID != "" {
			count++
		}

		if metadata.Parent == "" || metadata.Parent == yas.cfg.TrunkBranch {
			break
		}

		branch = metadata.Parent
	}

	// Count descendants (walking down from current)
	descendants := []string{}
	if err := yas.collectDescendants(graph, currentBranch, &descendants); err != nil {
		return 0, err
	}

	for _, descendantBranch := range descendants {
		metadata := yas.data.Branches.Get(descendantBranch)
		if metadata.GitHubPullRequest.ID != "" {
			count++
		}
	}

	return count, nil
}

func (yas *YAS) buildStackVisualization(currentBranch string) (string, error) {
	// Get the graph
	graph, err := yas.graph()
	if err != nil {
		return "", err
	}

	// Get ancestors (walking up to trunk)
	ancestors := []string{}

	branch := currentBranch
	for {
		metadata := yas.data.Branches.Get(branch)
		if metadata.Parent == "" || metadata.Parent == yas.cfg.TrunkBranch {
			break
		}

		ancestors = append([]string{metadata.Parent}, ancestors...)
		branch = metadata.Parent
	}

	// Get descendants (walking down from current)
	descendants := []string{}
	if err := yas.collectDescendants(graph, currentBranch, &descendants); err != nil {
		return "", err
	}

	// Build the visualization
	var lines []string

	indent := 0

	// Add ancestors
	for _, ancestorBranch := range ancestors {
		lines = append(lines, yas.formatStackLine(ancestorBranch, indent, false))
		indent++
	}

	// Add current branch
	lines = append(lines, yas.formatStackLine(currentBranch, indent, true))
	indent++

	// Add descendants
	for _, descendantBranch := range descendants {
		lines = append(lines, yas.formatStackLine(descendantBranch, indent, false))
		indent++
	}

	return strings.Join(lines, "\n"), nil
}

func (yas *YAS) collectDescendants(graph *dag.DAG, branchName string, descendants *[]string) error {
	children, err := graph.GetChildren(branchName)
	if err != nil {
		return err
	}

	for childID := range children {
		*descendants = append(*descendants, childID)
		if err := yas.collectDescendants(graph, childID, descendants); err != nil {
			return err
		}
	}

	return nil
}

func (yas *YAS) formatStackLine(branchName string, indent int, isCurrent bool) string {
	metadata := yas.data.Branches.Get(branchName)

	// Build the line
	line := strings.Repeat(" ", indent*2)
	line += "* "

	if metadata.GitHubPullRequest.ID != "" {
		// Just use the URL directly
		line += metadata.GitHubPullRequest.URL
	} else {
		// No PR, just show branch name
		line += branchName
	}

	if isCurrent {
		line += " 👈 (this PR)"
	}

	return line
}

func extractPRNumber(prURL string) string {
	parts := strings.Split(prURL, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return ""
}

func (yas *YAS) getPRBody(prNumber string) (string, error) {
	output, err := xexec.Command("gh", "pr", "view", prNumber, "--json", "body", "-q", ".body").WithStdout(nil).Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

func (yas *YAS) updatePRBody(prNumber, newBody string) error {
	return xexec.Command("gh", "pr", "edit", prNumber, "--body", newBody).Run()
}

func removeStackSection(currentBody string) string {
	stackMarkerAlt := "Stacked PRs:"

	// Check if there's already a stack section
	if idx := strings.Index(currentBody, stackMarkerAlt); idx != -1 {
		// Find the start of the stack section (look for --- before it)
		startIdx := idx
		if sepIdx := strings.LastIndex(currentBody[:idx], "---\n"); sepIdx != -1 {
			startIdx = sepIdx
		}
		// Remove the stack section
		currentBody = strings.TrimSpace(currentBody[:startIdx])
	}

	return currentBody
}

func updatePRBodyWithStack(currentBody, stackVisualization string) string {
	stackMarker := "---\n\nStacked PRs:\n\n"
	stackMarkerAlt := "Stacked PRs:"

	// Check if there's already a stack section
	if idx := strings.Index(currentBody, stackMarkerAlt); idx != -1 {
		// Find the start of the stack section (look for --- before it)
		startIdx := idx
		if sepIdx := strings.LastIndex(currentBody[:idx], "---\n"); sepIdx != -1 {
			startIdx = sepIdx
		}
		// Remove the old stack section
		currentBody = strings.TrimSpace(currentBody[:startIdx])
	}

	// Append the new stack section
	if currentBody != "" {
		currentBody += "\n\n"
	}

	currentBody += stackMarker + stackVisualization

	return currentBody
}

func (yas *YAS) TrackedBranches() Branches {
	return yas.data.Branches.ToSlice()
}

// UpdateConfig sets the new config and writes it to the configuration file.
func (yas *YAS) UpdateConfig(cfg Config) (string, error) {
	yas.cfg = cfg

	return WriteConfig(cfg)
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

func (yas *YAS) refreshRemoteStatus(name string) error {
	if strings.TrimSpace(name) == "" {
		panic("branch name cannot be empty")
	}

	pullRequestMetadata, err := yas.fetchGitHubPullRequestStatus(name)
	if err != nil {
		return err
	}

	if pullRequestMetadata == nil {
		pullRequestMetadata = &PullRequestMetadata{}
	}

	branchMetadata := yas.data.Branches.Get(name)

	branchMetadata.GitHubPullRequest = *pullRequestMetadata

	yas.data.Branches.Set(name, branchMetadata)

	// Set parent based on PR base ref name
	if branchMetadata.Parent == "" {
		branchMetadata.Parent = pullRequestMetadata.BaseRefName
	}

	if err := yas.data.Save(); err != nil {
		return err
	}

	return nil
}

func (yas *YAS) RefreshRemoteStatus(branchNames ...string) error {
	p := pool.New().WithMaxGoroutines(5).WithErrors().WithFirstError()
	for _, name := range branchNames {
		p.Go(func() error {
			return yas.refreshRemoteStatus(name)
		})
	}

	if err := p.Wait(); err != nil {
		return err
	}

	return nil
}

// refreshPRStatus refreshes PR status including review decision and CI checks.
func (yas *YAS) refreshPRStatus(name string) error {
	if strings.TrimSpace(name) == "" {
		panic("branch name cannot be empty")
	}

	pullRequestMetadata, err := yas.fetchPRStatusWithChecks(name)
	if err != nil {
		return err
	}

	if pullRequestMetadata == nil {
		pullRequestMetadata = &PullRequestMetadata{}
	}

	branchMetadata := yas.data.Branches.Get(name)
	branchMetadata.GitHubPullRequest = *pullRequestMetadata
	yas.data.Branches.Set(name, branchMetadata)

	if err := yas.data.Save(); err != nil {
		return err
	}

	return nil
}

// RefreshPRStatus refreshes PR status for multiple branches including review and CI status.
func (yas *YAS) RefreshPRStatus(branchNames ...string) error {
	p := pool.New().WithMaxGoroutines(5).WithErrors().WithFirstError()
	for _, name := range branchNames {
		p.Go(func() error {
			return yas.refreshPRStatus(name)
		})
	}

	if err := p.Wait(); err != nil {
		return err
	}

	return nil
}

func (yas *YAS) UpdateTrunk() error {
	if err := yas.git.QuietCheckout(yas.cfg.TrunkBranch); err != nil {
		return err
	}

	// Switch back to original branch
	defer func() {
		if err := yas.git.QuietCheckout("-"); err != nil {
			fmt.Fprintf(os.Stderr, "failed to checkout original branch: %v\n", err)
		}
	}()

	return yas.git.Pull()
}

func (yas *YAS) validate() error {
	gitVersion, err := yas.git.GitVersion()
	if err != nil {
		return fmt.Errorf("unable to determine git version: %w", err)
	}

	if gitVersion.LessThan(minimumRequiredGitVersion) {
		path, _ := yas.git.GitPath()

		return fmt.Errorf("git version %s (%s) is less than the required version %s", gitVersion.String(), path, minimumRequiredGitVersion.String())
	}

	return nil
}

// Merge merges the PR for the current branch using gh pr merge.
func (yas *YAS) Merge(force bool) error {
	// Check if a restack is in progress
	if err := yas.checkRestackInProgress(); err != nil {
		return err
	}

	// Get current branch
	currentBranch, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return err
	}

	if currentBranch == "HEAD" {
		return errors.New("cannot merge in detached HEAD state")
	}

	// Get branch metadata
	metadata := yas.data.Branches.Get(currentBranch)

	// Check that PR exists
	if metadata.GitHubPullRequest.ID == "" {
		return fmt.Errorf("branch '%s' does not have a PR", currentBranch)
	}

	// Check that branch is at top of stack (parent is trunk)
	if metadata.Parent != yas.cfg.TrunkBranch {
		return fmt.Errorf("branch must be at top of stack (parent must be %s, but is %s). Merge parent branches first", yas.cfg.TrunkBranch, metadata.Parent)
	}

	// Check that branch doesn't need restack
	needsRebase, err := yas.needsRebase(currentBranch, metadata.Parent)
	if err != nil {
		return fmt.Errorf("failed to check if branch needs restack: %w", err)
	}

	if needsRebase {
		return errors.New("branch needs restack. Run 'yas restack' first")
	}

	// Check that branch doesn't need submit
	needsSubmit, err := yas.needsSubmit(currentBranch)
	if err != nil {
		return fmt.Errorf("failed to check if branch needs submit: %w", err)
	}

	if needsSubmit {
		return errors.New("branch needs submit. Run 'yas submit' first")
	}

	// Check CI and review status (unless --force)
	if !force {
		pr, err := yas.fetchPRStatusWithChecks(currentBranch)
		if err != nil {
			return fmt.Errorf("failed to fetch PR status: %w", err)
		}

		if pr == nil {
			return errors.New("failed to fetch PR status")
		}

		ciStatus := pr.GetOverallCIStatus()
		if ciStatus != "SUCCESS" {
			return fmt.Errorf("CI checks are not passing (status: %s). Use --force to override", ciStatus)
		}

		if pr.ReviewDecision != "APPROVED" {
			return fmt.Errorf("PR needs approval (review status: %s). Use --force to override", pr.ReviewDecision)
		}
	}

	// Get PR number
	prNumber := extractPRNumber(metadata.GitHubPullRequest.URL)

	// Fetch PR title and body
	title, body, err := yas.getPRTitleAndBody(prNumber)
	if err != nil {
		return fmt.Errorf("failed to get PR title and body: %w", err)
	}

	// Strip stack section from body
	body = removeStackSection(body)

	// Write merge message to file
	mergeFilePath := path.Join(yas.cfg.RepoDirectory, ".git", "yas-merge-msg")

	mergeMsg := title + "\n\n" + body
	if err := os.WriteFile(mergeFilePath, []byte(mergeMsg), 0o644); err != nil {
		return fmt.Errorf("failed to write merge message file: %w", err)
	}

	// Open editor
	if err := yas.openEditor(mergeFilePath); err != nil {
		return fmt.Errorf("editor failed: %w", err)
	}

	// Read back the merge message
	finalMsg, err := os.ReadFile(mergeFilePath)
	if err != nil {
		return fmt.Errorf("failed to read merge message file: %w", err)
	}

	// Parse title and body
	finalTitle, finalBody := yas.parseMergeMessage(string(finalMsg))

	// Check if merge message is empty
	if strings.TrimSpace(finalTitle) == "" {
		// Clean up merge message file
		if err := os.Remove(mergeFilePath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to delete merge message file: %v\n", err)
		}

		return errors.New("merge aborted: empty commit message")
	}

	// Execute gh pr merge
	//	if err := xexec.Command("gh", "pr", "merge", prNumber, "--squash", "--delete-branch", "--auto", "--subject", finalTitle, "--body", finalBody).Run(); err != nil {
	if err := xexec.Command("gh", "pr", "merge", prNumber, "--squash", "--auto", "--subject", finalTitle, "--body", finalBody).Run(); err != nil {
		return fmt.Errorf("failed to merge PR: %w", err)
	}

	// Delete merge message file
	if err := os.Remove(mergeFilePath); err != nil {
		// Don't fail if cleanup fails
		fmt.Fprintf(os.Stderr, "Warning: failed to delete merge message file: %v\n", err)
	}

	fmt.Printf("\nPR #%s merged successfully!\n", prNumber)
	fmt.Printf("\nRun 'yas sync' to restack locally.\n")

	return nil
}

// getPRTitleAndBody fetches the PR title and body using gh pr view.
func (yas *YAS) getPRTitleAndBody(prNumber string) (string, string, error) {
	output, err := xexec.Command("gh", "pr", "view", prNumber, "--json", "title,body", "-q", ".title + \"\n---SEPARATOR---\n\" + .body").WithStdout(nil).Output()
	if err != nil {
		return "", "", err
	}

	parts := strings.Split(strings.TrimSpace(string(output)), "\n---SEPARATOR---\n")
	if len(parts) != 2 {
		return "", "", errors.New("unexpected gh pr view output format")
	}

	return parts[0], parts[1], nil
}

// openEditor opens the user's editor for the given file path.
func (yas *YAS) openEditor(filePath string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}

	cmd := xexec.Command(editor, filePath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// parseMergeMessage parses a merge message file into title and body.
// First line is the title, rest is the body (after skipping blank line).
func (yas *YAS) parseMergeMessage(msg string) (string, string) {
	lines := strings.Split(msg, "\n")
	if len(lines) == 0 {
		return "", ""
	}

	title := strings.TrimSpace(lines[0])

	// Find the start of the body (skip blank lines after title)
	bodyStart := 1
	for bodyStart < len(lines) && strings.TrimSpace(lines[bodyStart]) == "" {
		bodyStart++
	}

	if bodyStart >= len(lines) {
		return title, ""
	}

	body := strings.TrimSpace(strings.Join(lines[bodyStart:], "\n"))

	return title, body
}
