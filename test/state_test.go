package test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

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

	data, err := os.ReadFile(".yas/yas.state.json")
	assert.NilError(t, err)

	var state yasState
	assert.NilError(t, json.Unmarshal(data, &state))

	return state
}

func writeStateFile(t *testing.T, state yasState) {
	t.Helper()

	data, err := json.Marshal(state)
	assert.NilError(t, err)

	assert.NilError(t, os.MkdirAll(".yas", 0o755))
	assert.NilError(t, os.WriteFile(".yas/yas.state.json", data, 0o644))
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
		assert.Equal(t, yascli.Run("add", "feature/prune-me", "--parent=main"), 0)

		// Set the branch creation date to 8 days ago (older than the 7-day threshold)
		state := readStateFile(t)
		branch := state.Branches["feature/prune-me"]
		branch.Created = time.Now().Add(-8 * 24 * time.Hour)
		state.Branches["feature/prune-me"] = branch
		writeStateFile(t, state)

		state = readStateFile(t)
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

func TestDoesNotPruneRecentlyCreatedMissingBranches(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
                        git init --initial-branch=main

                        touch main
                        git add main
                        git commit -m "main-0"

                        git checkout -b feature/keep-me
                        touch feature
                        git add feature
                        git commit -m "feature-0"
                `)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "feature/keep-me", "--parent=main"), 0)

		// Set the branch creation date to 6 days ago (within the 7-day threshold)
		state := readStateFile(t)
		branch := state.Branches["feature/keep-me"]
		branch.Created = time.Now().Add(-6 * 24 * time.Hour)
		state.Branches["feature/keep-me"] = branch
		writeStateFile(t, state)

		state = readStateFile(t)
		if _, ok := state.Branches["feature/keep-me"]; !ok {
			t.Fatalf("expected branch to exist in state before pruning")
		}

		testutil.ExecOrFail(t, `
                        git checkout main
                        git branch -D feature/keep-me
                `)

		_, err := yas.NewFromRepository(".")
		assert.NilError(t, err)

		state = readStateFile(t)
		if _, ok := state.Branches["feature/keep-me"]; !ok {
			t.Fatalf("expected branch to still exist in state (not pruned) because it was created less than 7 days ago")
		}
	})
}
