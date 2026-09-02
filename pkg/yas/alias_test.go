package yas

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestResolveTrunkAlias(t *testing.T) {
	t.Parallel()

	y := &YAS{cfg: Config{
		TrunkBranch:        "main",
		TrunkBranchAliases: []string{"master", "trunk"},
	}}

	// Exact alias matches are replaced with the trunk branch
	assert.Equal(t, y.ResolveTrunkAlias("master"), "main")
	assert.Equal(t, y.ResolveTrunkAlias("trunk"), "main")

	// Anything else is passed through untouched
	assert.Equal(t, y.ResolveTrunkAlias("main"), "main")
	assert.Equal(t, y.ResolveTrunkAlias("feature-a"), "feature-a")
	assert.Equal(t, y.ResolveTrunkAlias(""), "")

	// Matching is exact, not prefix/suffix based
	assert.Equal(t, y.ResolveTrunkAlias("master-2"), "master-2")
	assert.Equal(t, y.ResolveTrunkAlias("Master"), "Master")
}

func TestResolveTrunkAlias_NoAliasesConfigured(t *testing.T) {
	t.Parallel()

	y := &YAS{cfg: Config{TrunkBranch: "main"}}

	assert.Equal(t, y.ResolveTrunkAlias("master"), "master")
}

func TestResolveTrunkAliases(t *testing.T) {
	t.Parallel()

	y := &YAS{cfg: Config{
		TrunkBranch:        "main",
		TrunkBranchAliases: []string{"master"},
	}}

	assert.DeepEqual(t, y.ResolveTrunkAliases([]string{"master", "feature-a", "master"}), []string{"main", "feature-a", "main"})
	assert.DeepEqual(t, y.ResolveTrunkAliases([]string{}), []string{})
	assert.Assert(t, y.ResolveTrunkAliases(nil) == nil)
}
