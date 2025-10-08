package yas

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/heimdalr/dag"
	"github.com/xlab/treeprint"
)

func formatBranchName(branchName string) string {
	lastSlash := strings.LastIndex(branchName, "/")
	if lastSlash == -1 {
		return branchName
	}

	prefix := branchName[:lastSlash]
	suffix := branchName[lastSlash+1:]
	if suffix == "" {
		return branchName
	}

	darkGray := color.New(color.FgHiBlack).SprintFunc()
	return fmt.Sprintf("%s%s", darkGray(prefix+"/"), suffix)
}

func (yas *YAS) addNodesFromGraph(treeNode treeprint.Tree, graph *dag.DAG, parentID string, currentBranch string, showStatus bool) error {
	children, err := graph.GetChildren(parentID)
	if err != nil {
		return err
	}

	for childID := range children {
		branchLabel := formatBranchName(childID)
		branchMetadata := yas.data.Branches.Get(childID)

		// Check if this branch needs rebasing or submitting
		var statusParts []string
		yellow := color.New(color.FgYellow).SprintFunc()

		needsRebase, err := yas.needsRebase(childID, parentID)
		if err != nil {
			// If we can't determine rebase status, just show the branch name
		} else if needsRebase {
			statusParts = append(statusParts, "needs restack")
		}

		// Check submit status
		if branchMetadata.GitHubPullRequest.ID == "" {
			// No PR exists
			statusParts = append(statusParts, "not submitted")
		} else {
			// PR exists, check if it needs updating
			needsSubmit, err := yas.needsSubmit(childID)
			if err != nil {
				// If we can't determine submit status, ignore
			} else if needsSubmit {
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

		// Add star at the end if this is the current branch
		if childID == currentBranch {
			darkGray := color.New(color.FgHiBlack).SprintFunc()
			branchLabel = fmt.Sprintf("%s %s", branchLabel, darkGray("*"))
		}

		childTree := treeNode.AddBranch(branchLabel)
		if err := yas.addNodesFromGraph(childTree, graph, childID, currentBranch, showStatus); err != nil {
			return err
		}
	}

	return nil
}

func getReviewStatusIcon(reviewDecision string) string {
	switch reviewDecision {
	case "APPROVED":
		return "✅"
	case "CHANGES_REQUESTED":
		return "❌"
	case "REVIEW_REQUIRED":
		return "❌"
	default:
		return "❌" // Default to cross for any other state
	}
}

func getCIStatusIcon(ciStatus string) string {
	switch ciStatus {
	case "SUCCESS":
		return "✅"
	case "FAILURE":
		return "❌"
	case "PENDING":
		return "⏳"
	case "": // No checks configured
		return "—" // Em dash to indicate no checks
	default:
		return "❌" // Default to cross for any other state
	}
}
