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

func (yas *YAS) addNodesFromGraph(treeNode treeprint.Tree, graph *dag.DAG, parentID string, currentBranch string) error {
	children, err := graph.GetChildren(parentID)
	if err != nil {
		return err
	}

	for childID := range children {
		branchLabel := formatBranchName(childID)

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
			cyan := color.New(color.FgCyan).SprintFunc()
			branchLabel = fmt.Sprintf("%s %s", branchLabel, cyan(fmt.Sprintf("[%s]", pr.URL)))
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
