package yas

import (
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/xlab/treeprint"
)

var minimumRequiredGitVersion = version.Must(version.NewVersion("2.38"))

const yasStateFile = ".git/.yasstate"

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
		if err := yas.git.Checkout(yas.cfg.TrunkBranch); err != nil {
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

// DeleteMergedBranch deletes a merged branch after restacking its children onto its parent
func (yas *YAS) DeleteMergedBranch(name string) error {
	// Get the metadata of the branch being deleted
	branchMetadata := yas.data.Branches.Get(name)
	parentBranch := branchMetadata.Parent

	// If no parent, just delete normally
	if parentBranch == "" {
		return yas.DeleteBranch(name)
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
			// Rebase the child onto the grandparent, removing commits from the merged branch
			// git rebase --onto <newbase> <oldbase> <branch>
			// This takes all commits in childID that are not in name, and rebases them onto parentBranch
			fmt.Printf("  Rebasing %s onto %s...\n", childID, parentBranch)
			if err := yas.git.RebaseOnto(parentBranch, name, childID); err != nil {
				return fmt.Errorf("failed to rebase %s onto %s: %w", childID, parentBranch, err)
			}

			// Update the child's parent to point to the grandparent
			childMetadata := yas.data.Branches.Get(childID)
			childMetadata.Parent = parentBranch
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

	b, err := xexec.Command("gh", "pr", "list", "--head", branchName, "--state", "all", "--json", "id,state,url,isDraft").WithStdout(nil).Output()
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

	trunkBranch := yas.data.Branches.Get(yas.cfg.TrunkBranch)
	graph.AddVertexByID(yas.cfg.TrunkBranch, trunkBranch)

	for _, branch := range yas.data.Branches.ToSlice().WithParents() {
		graph.AddVertexByID(branch.Name, branch) // TODO handle errors
	}

	for _, branch := range yas.data.Branches.ToSlice().WithParents() {
		graph.AddEdge(branch.Parent, branch.Name) // TODO handle errors
	}

	return graph, nil
}

// currentStackGraph returns a subgraph containing only the current stack:
// - Upwards: only parents in the current lineage to the trunk branch
// - Downwards: all descendants, including those with multiple children
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
		stackGraph.AddVertexByID(id, vertex)
	}

	// Add edges between vertices that are both in the stack
	for id := range stackVertices {
		children, err := fullGraph.GetChildren(id)
		if err != nil {
			return nil, err
		}
		for childID := range children {
			if stackVertices[childID] {
				stackGraph.AddEdge(id, childID)
			}
		}
	}

	return stackGraph, nil
}

// Restack rebases all branches starting from trunk, including all descendants
// and forks.
func (yas *YAS) Restack() error {
	// Remember the starting branch
	startingBranch, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return err
	}

	graph, err := yas.graph()
	if err != nil {
		return err
	}

	// Track which branches were rebased
	rebasedBranches := []string{}

	// Start from trunk and rebase all descendants recursively
	if err := yas.rebaseDescendants(graph, yas.cfg.TrunkBranch, &rebasedBranches); err != nil {
		return err
	}

	// Return to the starting branch
	if err := yas.git.Checkout(startingBranch); err != nil {
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

func (yas *YAS) rebaseDescendants(graph *dag.DAG, branchName string, rebasedBranches *[]string) error {
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
			// Rebase child onto parent (branchName)
			if err := yas.git.Rebase(branchName, childID); err != nil {
				return err
			}

			// Track that this branch was rebased
			*rebasedBranches = append(*rebasedBranches, childID)
		}

		// Recursively rebase this child's descendants
		if err := yas.rebaseDescendants(graph, childID, rebasedBranches); err != nil {
			return err
		}
	}

	return nil
}

func (yas *YAS) toTree(graph *dag.DAG, rootNode string, currentBranch string) (treeprint.Tree, error) {
	rootLabel := formatBranchName(rootNode)

	// Add star at the end if trunk is the current branch
	if rootNode == currentBranch {
		darkGray := color.New(color.FgHiBlack).SprintFunc()
		rootLabel = fmt.Sprintf("%s %s", rootLabel, darkGray("*"))
	}

	tree := treeprint.NewWithRoot(rootLabel)

	if err := yas.addNodesFromGraph(tree, graph, rootNode, currentBranch); err != nil {
		return nil, err
	}

	return tree, nil
}

func (yas *YAS) needsRebase(branchName, parentBranch string) (bool, error) {
	// Get the merge base between the branch and its parent
	mergeBase, err := yas.git.GetMergeBase(branchName, parentBranch)
	if err != nil {
		return false, err
	}

	// Get the current commit of the parent branch
	parentCommit, err := yas.git.GetCommitHash(parentBranch)
	if err != nil {
		return false, err
	}

	// If merge base is different from parent's current commit, rebase is needed
	return mergeBase != parentCommit, nil
}

func (yas *YAS) List(currentStackOnly bool) error {
	graph, err := yas.graph()
	if err != nil {
		return fmt.Errorf("failed to get graph: %w", err)
	}

	currentBranch, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return err
	}

	// Filter to current stack if requested
	if currentStackOnly {
		graph, err = yas.currentStackGraph(graph, currentBranch)
		if err != nil {
			return fmt.Errorf("failed to get current stack: %w", err)
		}
	}

	tree, err := yas.toTree(graph, yas.cfg.TrunkBranch, currentBranch)
	if err != nil {
		return err
	}

	fmt.Print(tree.String())

	return nil
}

func (yas *YAS) SetParent(branchName, parentBranchName string) error {
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
	yas.data.Branches.Set(branchName, branchMetdata)
	yas.data.Save()

	fmt.Printf("Set '%s' as parent of '%s'\n", parentBranchName, branchName)

	return nil
}

func (yas *YAS) Submit() error {
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

func (yas *YAS) SubmitStack() error {
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
	var oldRemoteHash string
	var remoteExists bool
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

		// Show appropriate message based on what happened
		if !needsPush {
			fmt.Printf("PR exists: %s (state: %s), up to date\n",
				metadata.GitHubPullRequest.URL,
				state)
		} else if oldRemoteHash != "" && oldRemoteHash != currentLocalHash {
			fmt.Printf("PR exists: %s (state: %s), force pushed (was: %s)\n",
				metadata.GitHubPullRequest.URL,
				state,
				oldRemoteHash)
		} else {
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

	// Build the stack visualization
	stackVisualization, err := yas.buildStackVisualization(branchName)
	if err != nil {
		return fmt.Errorf("failed to build stack visualization: %w", err)
	}

	// Get the current PR body
	prNumber := extractPRNumber(metadata.GitHubPullRequest.URL)
	currentBody, err := yas.getPRBody(prNumber)
	if err != nil {
		return fmt.Errorf("failed to get PR body: %w", err)
	}

	// Update the body with the stack section
	newBody := updatePRBodyWithStack(currentBody, stackVisualization)

	// Update the PR
	if err := yas.updatePRBody(prNumber, newBody); err != nil {
		return fmt.Errorf("failed to update PR: %w", err)
	}

	fmt.Printf("Updated PR #%s with stack information\n", prNumber)
	return nil
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
	iter.ForEach(func(r *plumbing.Reference) error {
		name := string(r.Name().Short())
		if !yas.data.Branches.Exists(name) {
			branches = append(branches, name)
		}
		return nil
	})

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

func (yas *YAS) UpdateTrunk() error {
	if err := yas.git.Checkout(yas.cfg.TrunkBranch); err != nil {
		return err
	}

	// Switch back to original branch
	defer yas.git.Checkout("-")

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
