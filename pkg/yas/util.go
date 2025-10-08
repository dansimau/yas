package yas

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/heimdalr/dag"
	"github.com/xlab/treeprint"
)

func (yas *YAS) addNodesFromGraph(treeNode treeprint.Tree, graph *dag.DAG, parentID string, currentBranch string) error {
	children, err := graph.GetChildren(parentID)
	if err != nil {
		return err
	}

	for childID := range children {
		branchLabel := childID

		// Check if this branch needs rebasing
		needsRebase, err := yas.needsRebase(childID, parentID)
		if err != nil {
			// If we can't determine rebase status, just show the branch name
		} else if needsRebase {
			yellow := color.New(color.FgYellow).SprintFunc()
			branchLabel = fmt.Sprintf("%s %s", branchLabel, yellow("(needs restack)"))
		}

		// Add PR information if available
		branchMetadata := yas.data.Branches.Get(childID)
		if branchMetadata.GitHubPullRequest.ID != "" {
			pr := branchMetadata.GitHubPullRequest
			state := pr.State
			if pr.IsDraft {
				state = "DRAFT"
			}
			cyan := color.New(color.FgCyan).SprintFunc()
			branchLabel = fmt.Sprintf("%s %s", branchLabel, cyan(fmt.Sprintf("[%s: %s]", state, pr.URL)))
		}

		// Add star at the end if this is the current branch
		if childID == currentBranch {
			darkGray := color.New(color.FgHiBlack).SprintFunc()
			branchLabel = fmt.Sprintf("%s %s", branchLabel, darkGray("*"))
		}

		childTree := treeNode.AddBranch(branchLabel)
		if err := yas.addNodesFromGraph(childTree, graph, childID, currentBranch); err != nil {
			return err
		}
	}

	return nil
}
