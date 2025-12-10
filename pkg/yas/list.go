package yas

import (
	"fmt"
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/heimdalr/dag"
)

func (yas *YAS) List(currentStackOnly bool, showStatus bool, showAll bool) error {
	items, err := yas.GetBranchList(currentStackOnly, showStatus, showAll)
	if err != nil {
		return err
	}

	for _, item := range items {
		fmt.Println(item.Line)
	}

	return nil
}

// augmentGraphWithAllBranches adds all local git branches to the graph,
// detecting their parent branches using git helpers.
func (yas *YAS) augmentGraphWithAllBranches(graph *dag.DAG) error {
	// Get all local branches
	allBranches, err := yas.GetAllLocalBranches()
	if err != nil {
		return fmt.Errorf("failed to get all local branches: %w", err)
	}

	// Add vertices for branches not already in the graph
	for _, branchName := range allBranches {
		// Skip if already in graph (either trunk or tracked branch)
		if _, err := graph.GetVertex(branchName); err == nil {
			continue
		}

		// Add vertex for this untracked branch
		if err := graph.AddVertexByID(branchName, branchName); err != nil {
			return fmt.Errorf("failed to add vertex for %s: %w", branchName, err)
		}
	}

	// Add edges for untracked branches
	for _, branchName := range allBranches {
		// Skip if this is a tracked branch (it already has edges)
		if yas.data.Branches.Get(branchName).Parent != "" {
			continue
		}

		// Skip trunk branch
		if branchName == yas.cfg.TrunkBranch {
			continue
		}

		// Detect parent branch
		parentBranch := yas.detectParentBranch(branchName)

		// Ensure parent exists in graph
		if _, err := graph.GetVertex(parentBranch); err != nil {
			// Parent not in graph, skip
			continue
		}

		// Add edge from parent to this branch
		if err := graph.AddEdge(parentBranch, branchName); err != nil {
			// Edge might already exist or create a cycle, skip
			continue
		}
	}

	return nil
}

// GetBranchList returns a list of SelectionItems representing all branches
// Each item contains the branch name (ID) and the formatted display line.
func (yas *YAS) GetBranchList(currentStackOnly bool, showStatus bool, showAll bool) ([]SelectionItem, error) {
	graph, err := yas.graph()
	if err != nil {
		return nil, fmt.Errorf("failed to get graph: %w", err)
	}

	currentBranch, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return nil, err
	}

	// If showAll flag is set, augment graph with untracked branches
	if showAll {
		if err := yas.augmentGraphWithAllBranches(graph); err != nil {
			return nil, fmt.Errorf("failed to augment graph with all branches: %w", err)
		}
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

	// Sort by Created timestamp (ascending), then by name alphabetically
	// Untracked branches (without metadata) are sorted alphabetically after tracked ones
	sort.Slice(childIDs, func(i, j int) bool {
		metaI := yas.data.Branches.Get(childIDs[i])
		metaJ := yas.data.Branches.Get(childIDs[j])

		// Check if branches are tracked
		isTrackedI := !metaI.Created.IsZero()
		isTrackedJ := !metaJ.Created.IsZero()

		// If one is tracked and the other isn't, tracked comes first
		if isTrackedI != isTrackedJ {
			return isTrackedI
		}

		// Both are tracked: sort by timestamp (older first)
		if isTrackedI && isTrackedJ {
			// If timestamps differ, sort by timestamp
			if !metaI.Created.Equal(metaJ.Created) {
				return metaI.Created.Before(metaJ.Created)
			}
		}

		// Both untracked or timestamps are equal: sort by name alphabetically
		return childIDs[i] < childIDs[j]
	})

	for i, childID := range childIDs {
		isLastChild := i == len(childIDs)-1

		// Check if this is a tracked or untracked branch
		branchMetadata := yas.data.Branches.Get(childID)
		isTracked := branchMetadata.Parent != ""

		// Build branch label
		var branchLabel string
		if isTracked {
			// Tracked branch: use normal formatting
			branchLabel = formatBranchName(childID)
		} else {
			// Untracked branch: grey out the entire name
			darkGray := color.New(color.FgHiBlack).SprintFunc()
			branchLabel = darkGray(childID)
		}

		// Only add status information for tracked branches
		if isTracked {
			// Check if this branch needs rebasing or submitting
			var statusParts []string

			yellow := color.New(color.FgYellow).SprintFunc()

			branchExists, err := yas.git.BranchExists(childID)
			if err != nil {
				return err
			}

			if !branchExists {
				statusParts = append(statusParts, "deleted")
			} else {
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
