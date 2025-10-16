package yas

import (
	"github.com/heimdalr/dag"
)

// branchVisitor implements the dag.Visitor interface to collect branch names.
type branchVisitor struct {
	branches    *[]string
	trunkBranch string
}

func (bv *branchVisitor) Visit(v dag.Vertexer) {
	_, value := v.Vertex()
	if branchName, ok := value.(string); ok {
		// Skip trunk branch if present
		if branchName != bv.trunkBranch {
			*bv.branches = append(*bv.branches, branchName)
		}
	}
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
