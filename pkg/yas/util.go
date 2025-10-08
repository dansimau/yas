package yas

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/heimdalr/dag"
	"github.com/xlab/treeprint"
)

func (yas *YAS) addNodesFromGraph(treeNode treeprint.Tree, graph *dag.DAG, parentID string) error {
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
			branchLabel = childID
		} else if needsRebase {
			yellow := color.New(color.FgYellow).SprintFunc()
			branchLabel = fmt.Sprintf("%s %s", childID, yellow("(needs restack)"))
		}

		childTree := treeNode.AddBranch(branchLabel)
		if err := yas.addNodesFromGraph(childTree, graph, childID); err != nil {
			return err
		}
	}

	return nil
}
