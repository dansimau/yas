package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

// setupFakeGH creates a fake gh script that logs commands to a file
func setupFakeGH(t *testing.T) (ghLogFile string, cleanup func()) {
	t.Helper()

	// Create temp directory for fake gh script
	tmpDir, err := os.MkdirTemp("", "yas-test-gh-*")
	assert.NilError(t, err)

	// Create log file
	ghLogFile = filepath.Join(tmpDir, "gh.log")

	// Create fake gh script
	fakeGH := filepath.Join(tmpDir, "gh")
	ghScript := `#!/bin/bash
echo "$@" >> "` + ghLogFile + `"
# Simulate gh pr list output (empty - no PRs found)
if [[ "$1" == "pr" && "$2" == "list" ]]; then
  echo "[]"
fi
exit 0
`
	err = os.WriteFile(fakeGH, []byte(ghScript), 0o755)
	assert.NilError(t, err)

	// Prepend fake gh to PATH
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", tmpDir+":"+oldPath)

	cleanup = func() {
		os.Setenv("PATH", oldPath)
		os.RemoveAll(tmpDir)
	}

	return ghLogFile, cleanup
}

func TestSubmit_SkipsCreatingPRWhenAlreadyExists(t *testing.T) {
	ghLogFile, cleanup := setupFakeGH(t)
	defer cleanup()

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git remote add origin https://github.com/test/test.git

			# main
			touch main
			git add main
			git commit -m "main-0"

			# topic-a
			git checkout -b topic-a
			touch a
			git add a
			git commit -m "topic-a-0"
		`)

		// Initialize yas config
		cfg := yas.Config{
			RepoDirectory: ".",
			TrunkBranch:   "main",
		}
		_, err := yas.WriteConfig(cfg)
		assert.NilError(t, err)

		// Create YAS instance
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)

		// Track the branch
		err = y.SetParent("topic-a", "main")
		assert.NilError(t, err)

		// Simulate an existing PR by manually editing the state file
		stateFile := ".git/.yasstate"
		stateData, err := os.ReadFile(stateFile)
		assert.NilError(t, err)

		type pullRequestMetadata struct {
			ID    string
			State string
		}
		type branchMetadata struct {
			GitHubPullRequest pullRequestMetadata
			Parent            string `json:",omitempty"`
		}
		var state struct {
			Branches map[string]branchMetadata
		}
		err = json.Unmarshal(stateData, &state)
		assert.NilError(t, err)

		// Set PR metadata to simulate existing PR
		if branch, exists := state.Branches["topic-a"]; exists {
			branch.GitHubPullRequest.ID = "PR_test123"
			branch.GitHubPullRequest.State = "OPEN"
			state.Branches["topic-a"] = branch
		}

		// Write back
		updatedData, err := json.MarshalIndent(state, "", "  ")
		assert.NilError(t, err)
		err = os.WriteFile(stateFile, updatedData, 0o644)
		assert.NilError(t, err)

		// Reload YAS to pick up the changes
		y, err = yas.NewFromRepository(".")
		assert.NilError(t, err)

		// Verify PR metadata is in the state file
		stateData2, err := os.ReadFile(stateFile)
		assert.NilError(t, err)
		var state2 struct {
			Branches map[string]branchMetadata
		}
		err = json.Unmarshal(stateData2, &state2)
		assert.NilError(t, err)
		assert.Equal(t, state2.Branches["topic-a"].GitHubPullRequest.ID, "PR_test123")

		// Call submit - should skip creating PR
		err = y.Submit()
		// Push will fail but that's ok, we're testing the PR creation logic
		assert.ErrorContains(t, err, "failed to push")

		// Verify gh pr create was NOT called
		ghLog, err := os.ReadFile(ghLogFile)
		if err == nil {
			logContent := string(ghLog)
			assert.Assert(t, !strings.Contains(logContent, "pr create"),
				"gh pr create should NOT be called when PR exists, but got: %s", logContent)
		}
	})
}

func TestSubmit_CreatesNewPRWhenNoneExists(t *testing.T) {
	ghLogFile, cleanup := setupFakeGH(t)
	defer cleanup()

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git remote add origin https://github.com/test/test.git

			# main
			touch main
			git add main
			git commit -m "main-0"

			# topic-a
			git checkout -b topic-a
			touch a
			git add a
			git commit -m "topic-a-0"
		`)

		// Initialize yas config
		cfg := yas.Config{
			RepoDirectory: ".",
			TrunkBranch:   "main",
		}
		_, err := yas.WriteConfig(cfg)
		assert.NilError(t, err)

		// Create YAS instance
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)

		// Track the branch
		err = y.SetParent("topic-a", "main")
		assert.NilError(t, err)

		// Call submit - should try to create PR
		err = y.Submit()
		// Push will fail but that's ok
		assert.ErrorContains(t, err, "failed to push")

		// Verify gh pr list was called (to check for existing PR)
		ghLog, err := os.ReadFile(ghLogFile)
		assert.NilError(t, err)
		logContent := string(ghLog)
		assert.Assert(t, strings.Contains(logContent, "pr list"),
			"gh pr list should be called, but got: %s", logContent)
	})
}
