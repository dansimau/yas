package yas

import (
	"github.com/heimdalr/dag"
	"github.com/xlab/treeprint"
)

func addNodesFromGraph(treeNode treeprint.Tree, graph *dag.DAG, vertexID string) error {
	children, err := graph.GetChildren(vertexID)
	if err != nil {
		return err
	}

	for child := range children {
		childTree := treeNode.AddBranch(child)
		if err := addNodesFromGraph(childTree, graph, child); err != nil {
			return err
		}
	}

	return nil
}
