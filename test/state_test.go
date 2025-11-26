package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

type yasState struct {
	Branches map[string]yas.BranchMetadata `json:"branches"`
}

func readStateFileFromDir(t *testing.T, dir string) yasState {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dir, ".yas/yas.state.json"))
	assert.NilError(t, err)

	var state yasState
	assert.NilError(t, json.Unmarshal(data, &state))

	return state
}

func writeStateFileToDir(t *testing.T, dir string, state yasState) {
	t.Helper()

	data, err := json.Marshal(state)
	assert.NilError(t, err)

	assert.NilError(t, os.MkdirAll(filepath.Join(dir, ".yas"), 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(dir, ".yas/yas.state.json"), data, 0o644))
}

func TestPrunesBranchesMissingLocally(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		touch main
		git add main
		git commit -m "main-0"

		git checkout -b feature/prune-me
		touch feature
		git add feature
		git commit -m "feature-0"
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature/prune-me", "--parent=main").Err())

	// Set the branch creation date to 8 days ago (older than the 7-day threshold)
	state := readStateFileFromDir(t, tempDir)
	branch := state.Branches["feature/prune-me"]
	branch.Created = time.Now().Add(-8 * 24 * time.Hour)
	state.Branches["feature/prune-me"] = branch
	writeStateFileToDir(t, tempDir, state)

	state = readStateFileFromDir(t, tempDir)
	if _, ok := state.Branches["feature/prune-me"]; !ok {
		t.Fatalf("expected branch to exist in state before pruning")
	}

	testutil.ExecOrFail(t, tempDir, `
		git checkout main
		git branch -D feature/prune-me
	`)

	_, err := yas.NewFromRepository(tempDir)
	assert.NilError(t, err)

	state = readStateFileFromDir(t, tempDir)
	if _, ok := state.Branches["feature/prune-me"]; ok {
		t.Fatalf("expected branch to be pruned from state after deletion")
	}
}

func TestDoesNotPruneRecentlyCreatedMissingBranches(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		touch main
		git add main
		git commit -m "main-0"

		git checkout -b feature/keep-me
		touch feature
		git add feature
		git commit -m "feature-0"
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature/keep-me", "--parent=main").Err())

	// Set the branch creation date to 6 days ago (within the 7-day threshold)
	state := readStateFileFromDir(t, tempDir)
	branch := state.Branches["feature/keep-me"]
	branch.Created = time.Now().Add(-6 * 24 * time.Hour)
	state.Branches["feature/keep-me"] = branch
	writeStateFileToDir(t, tempDir, state)

	state = readStateFileFromDir(t, tempDir)
	if _, ok := state.Branches["feature/keep-me"]; !ok {
		t.Fatalf("expected branch to exist in state before pruning")
	}

	testutil.ExecOrFail(t, tempDir, `
		git checkout main
		git branch -D feature/keep-me
	`)

	_, err := yas.NewFromRepository(tempDir)
	assert.NilError(t, err)

	state = readStateFileFromDir(t, tempDir)
	if _, ok := state.Branches["feature/keep-me"]; !ok {
		t.Fatalf("expected branch to still exist in state (not pruned) because it was created less than 7 days ago")
	}
}
