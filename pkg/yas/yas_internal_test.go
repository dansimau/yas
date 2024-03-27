package yas

// func TestDAG(t *testing.T) {
// 	graph := dag.NewDAG()

// 	assert.NilError(t, graph.AddVertexByID("develop", "develop"))
// 	assert.NilError(t, graph.AddVertexByID("feature-A", "feature-A"))
// 	assert.NilError(t, graph.AddVertexByID("feature-B", "feature-B"))

// 	assert.NilError(t, graph.AddEdge("develop", "feature-A"))
// 	assert.NilError(t, graph.AddEdge("feature-A", "feature-B"))

// 	spew.Dump("leaves", graph.GetLeaves())
// 	spew.Dump(graph.GetChildren("feature-A"))

// 	t.Fail()
// }

// func TestTree(t *testing.T) {
// 	tree := treeprint.New()

// 	tree.AddBranch("foo")
// 	tree.AddBranch("bar")

// 	println(tree.String())

// 	t.Fail()
// }
