package test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"github.com/dansimau/yas/pkg/yascli"
	"gotest.tools/v3/assert"
)

type yasState struct {
	Branches map[string]yas.BranchMetadata `json:"branches"`
}

func readStateFile(t *testing.T) yasState {
	t.Helper()

	data, err := os.ReadFile(".git/.yasstate")
	assert.NilError(t, err)

	var state yasState
	assert.NilError(t, json.Unmarshal(data, &state))

	return state
}

func TestPrunesBranchesMissingLocally(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
                        git init --initial-branch=main

                        touch main
                        git add main
                        git commit -m "main-0"

                        git checkout -b feature/prune-me
                        touch feature
                        git add feature
                        git commit -m "feature-0"
                `)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=feature/prune-me", "--parent=main"), 0)

		state := readStateFile(t)
		if _, ok := state.Branches["feature/prune-me"]; !ok {
			t.Fatalf("expected branch to exist in state before pruning")
		}

		testutil.ExecOrFail(t, `
                        git checkout main
                        git branch -D feature/prune-me
                `)

		_, err := yas.NewFromRepository(".")
		assert.NilError(t, err)

		state = readStateFile(t)
		if _, ok := state.Branches["feature/prune-me"]; ok {
			t.Fatalf("expected branch to be pruned from state after deletion")
		}
	})
}
