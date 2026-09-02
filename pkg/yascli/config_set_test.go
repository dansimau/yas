package yascli

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestParseTrunkBranchAliases(t *testing.T) {
	t.Parallel()

	assert.DeepEqual(t, parseTrunkBranchAliases("master,trunk"), []string{"master", "trunk"})
	assert.DeepEqual(t, parseTrunkBranchAliases(" master , trunk ,, "), []string{"master", "trunk"})
	assert.Assert(t, parseTrunkBranchAliases("") == nil)
	assert.Assert(t, parseTrunkBranchAliases(" , ") == nil)
}
