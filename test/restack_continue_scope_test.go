package test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

// TestContinue_RespectsTargetBranchScope verifies that when `yas continue` is run
// after a conflict during a targeted restack (not --all), it only restacks the
// originally targeted branch and its descendants, not all branches from trunk.
func TestContinue_RespectsTargetBranchScope(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Create a stack: main -> topic-a -> topic-b -> topic-c
	// Then update main and topic-a such that both need rebasing
	// When we restack just topic-b, only topic-b and topic-c should be restacked
	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		# main: initial commit
		echo "main-initial" > main.txt
		git add main.txt
		git commit -m "main-0"

		# topic-a: branches from main
		git checkout -b topic-a
		echo "topic-a-content" > topic-a.txt
		git add topic-a.txt
		git commit -m "topic-a-0"

		# topic-b: branches from topic-a, with a file that will conflict
		git checkout -b topic-b
		echo "topic-b-original" > conflict.txt
		git add conflict.txt
		git commit -m "topic-b-0"

		# topic-c: branches from topic-b
		git checkout -b topic-c
		echo "topic-c-content" > topic-c.txt
		git add topic-c.txt
		git commit -m "topic-c-0"

		# Now update topic-a to add a conflicting change
		git checkout topic-a
		echo "topic-a-conflict" > conflict.txt
		git add conflict.txt
		git commit -m "topic-a-1"

		# Also update main so topic-a would need rebasing (if we were doing --all)
		git checkout main
		echo "main-updated" >> main.txt
		git add main.txt
		git commit -m "main-1"

		# Go back to topic-b
		git checkout topic-b
	`)

	// Initialize yas config and add all branches
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())
	assert.NilError(t, cli.Run("add", "topic-c", "--parent=topic-b").Err())

	// Get the branch points before restack
	topicABranchPointBefore := getBranchPoint(t, tempDir, "topic-a")

	// Run restack on topic-b only (not --all)
	// This should cause a conflict because topic-a has a conflicting change
	result := cli.Run("restack")
	assert.Equal(t, result.ExitCode(), 1, "restack should fail due to conflict")

	// Verify that restack state was saved with the target branch
	state := loadRestackState(t, tempDir)
	assert.Equal(t, state.TargetBranch, "topic-b", "target branch should be topic-b")

	// Resolve the conflict
	testutil.ExecOrFail(t, tempDir, `
		# Accept both versions
		echo "topic-a-conflict" > conflict.txt
		echo "topic-b-original" >> conflict.txt
		git add conflict.txt
		git rebase --continue
	`)

	// Run continue - it should only process topic-c, not topic-a
	result = cli.Run("continue")
	assert.NilError(t, result.Err(), "continue should succeed after conflict resolution")

	// Verify topic-a was NOT restacked (its branch point should be unchanged)
	topicABranchPointAfter := getBranchPoint(t, tempDir, "topic-a")
	assert.Equal(t, topicABranchPointBefore, topicABranchPointAfter,
		"topic-a should not have been restacked since it was outside the target scope")

	// Verify topic-b and topic-c were restacked (they should be descendants of topic-a)
	// We can verify this by checking that we're back on topic-b and the rebase completed
	currentBranch := mustExecOutput(tempDir, "git", "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, "topic-b\n", currentBranch, "should be back on starting branch topic-b")
}

// getBranchPoint reads the branch point for a branch from the yas state file.
func getBranchPoint(t *testing.T, repoDir, branchName string) string {
	t.Helper()

	stateFile := repoDir + "/.yas/yas.state.json"
	data, err := os.ReadFile(stateFile)
	assert.NilError(t, err, "should be able to read state file")

	var state struct {
		Branches map[string]yas.BranchMetadata `json:"branches"`
	}
	assert.NilError(t, json.Unmarshal(data, &state), "should be able to parse state file")

	branch, exists := state.Branches[branchName]
	if !exists {
		t.Fatalf("branch %s not found in state", branchName)
	}

	return branch.BranchPoint
}

// loadRestackState reads the restack state file.
func loadRestackState(t *testing.T, repoDir string) *yas.RestackState {
	t.Helper()

	restackStateFiles := []string{".yas/yas.restack.json", ".git/.yasrestack"}
	for _, filename := range restackStateFiles {
		fullPath := repoDir + "/" + filename

		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		var state yas.RestackState
		assert.NilError(t, json.Unmarshal(data, &state), "should be able to parse restack state file")

		return &state
	}

	t.Fatal("restack state file not found")

	return nil
}
