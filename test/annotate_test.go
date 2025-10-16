package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"github.com/dansimau/yas/pkg/yascli"
	"gotest.tools/v3/assert"
)

func TestAnnotate_UpdatesPRWithStackInfo(t *testing.T) {
	_, cleanup := setupMockCommandsWithPR(t, mockPROptions{
		ID:    "PR_kwDOTest123",
		State: "OPEN",
		URL:   "https://github.com/test/test/pull/42",
	})
	defer cleanup()

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git remote add origin https://fake.origin/test/test.git

			# main
			touch main
			git add main
			git commit -m "main-0"

			# topic-a
			git checkout -b topic-a
			touch a
			git add a
			git commit -m "topic-a-0"

			# topic-b (child of topic-a)
			git checkout -b topic-b
			touch b
			git add b
			git commit -m "topic-b-0"

			# topic-c (child of topic-b)
			git checkout -b topic-c
			touch c
			git add c
			git commit -m "topic-c-0"
		`)

		// Initialize yas config
		cfg := yas.Config{
			RepoDirectory: ".",
			TrunkBranch:   "main",
		}
		_, err := yas.WriteConfig(cfg)
		assert.NilError(t, err)

		// Create YAS instance and track branches
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)
		err = y.SetParent("topic-a", "main", "")
		assert.NilError(t, err)
		err = y.SetParent("topic-b", "topic-a", "")
		assert.NilError(t, err)
		err = y.SetParent("topic-c", "topic-b", "")
		assert.NilError(t, err)

		// Submit topic-b to create a PR
		testutil.ExecOrFail(t, "git checkout topic-b")

		err = y.Submit()
		assert.NilError(t, err)

		// Run annotate
		assert.Equal(t, yascli.Run("annotate"), 0)
	})
}

func TestAnnotate_ErrorWhenNoPR(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main

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

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=topic-a", "--parent=main"), 0)

		// Try to annotate without a PR - should fail
		exitCode := yascli.Run("annotate")
		assert.Assert(t, exitCode != 0, "Should fail when branch has no PR")
	})
}

func TestAnnotate_SinglePRInStack_DoesNotAddStackSection(t *testing.T) {
	prBody := "This is my PR description."

	_, cleanup := setupMockCommandsWithPRBody(t, mockPROptions{
		ID:    "PR_kwDOTest123",
		State: "OPEN",
		URL:   "https://github.com/test/test/pull/42",
	}, &prBody)
	defer cleanup()

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git remote add origin https://fake.origin/test/test.git

			# main
			touch main
			git add main
			git commit -m "main-0"

			# topic-a (only PR in stack)
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

		// Create YAS instance and track branch
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)
		err = y.SetParent("topic-a", "main", "")
		assert.NilError(t, err)

		// Submit to create a PR
		testutil.ExecOrFail(t, "git checkout topic-a")

		err = y.Submit()
		assert.NilError(t, err)

		// Run annotate - should not add stack section
		assert.Equal(t, yascli.Run("annotate"), 0)

		// Verify stack section was not added
		assert.Assert(t, !strings.Contains(prBody, "Stacked PRs:"), "Should not add stack section for single PR")
		assert.Assert(t, strings.Contains(prBody, "This is my PR description."), "Should preserve original description")
	})
}

func TestAnnotate_SinglePRInStack_RemovesExistingStackSection(t *testing.T) {
	prBody := `This is my PR description.

---

Stacked PRs:

* https://github.com/test/test/pull/42 👈 (this PR)`

	bodyFile, cleanup := setupMockCommandsWithPRBody(t, mockPROptions{
		ID:    "PR_kwDOTest123",
		State: "OPEN",
		URL:   "https://github.com/test/test/pull/42",
	}, &prBody)
	defer cleanup()

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git remote add origin https://fake.origin/test/test.git

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

		// Create YAS instance and track branch
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)
		err = y.SetParent("topic-a", "main", "")
		assert.NilError(t, err)

		// Submit to create a PR - this calls annotate internally
		testutil.ExecOrFail(t, "git checkout topic-a")

		err = y.Submit()
		assert.NilError(t, err)

		// Read the body file to get the result from submit's annotate call
		bodyBytes, err := os.ReadFile(bodyFile)
		assert.NilError(t, err)

		finalBody := string(bodyBytes)

		// Verify stack section was removed
		assert.Assert(t, !strings.Contains(finalBody, "Stacked PRs:"), "Should remove stack section for single PR, got: %s", finalBody)
		assert.Assert(t, strings.Contains(finalBody, "This is my PR description."), "Should preserve original description")
		assert.Assert(t, !strings.Contains(finalBody, "---"), "Should remove separator")
	})
}

// setupMockCommandsWithPRBody is like setupMockCommandsWithPR but also tracks PR body updates.
func setupMockCommandsWithPRBody(t *testing.T, pr mockPROptions, prBody *string) (prBodyFile string, cleanup func()) {
	t.Helper()

	// Create temp directory for mock commands
	tmpDir := t.TempDir()

	// Create command log file (not returned but needed for the mocks)
	_ = filepath.Join(tmpDir, "commands.log")

	// Create a wrapper script that handles PR body operations
	mockGH := filepath.Join(tmpDir, "gh")
	mockGHScript := `#!/bin/bash
if [[ "$1" == "pr" && "$2" == "list" ]]; then
	# Return existing PR if configured
	if [ -n "$YAS_TEST_EXISTING_PR_ID" ]; then
		state="${YAS_TEST_PR_STATE:-OPEN}"
		url="${YAS_TEST_PR_URL:-https://github.com/test/test/pull/1}"
		isDraft="${YAS_TEST_PR_IS_DRAFT:-false}"
		baseRefName="${YAS_TEST_PR_BASE_REF:-main}"
		echo "[{\"id\":\"$YAS_TEST_EXISTING_PR_ID\",\"state\":\"$state\",\"url\":\"$url\",\"isDraft\":$isDraft,\"baseRefName\":\"$baseRefName\",\"statusCheckRollup\":[]}]"
	else
		echo "[]"
	fi
	exit 0
elif [[ "$1" == "pr" && "$2" == "view" ]]; then
	cat "$YAS_TEST_PR_BODY_FILE"
	exit 0
elif [[ "$1" == "pr" && "$2" == "edit" ]]; then
	# Extract the --body argument
	for ((i=3; i<=$#; i++)); do
		if [[ "${!i}" == "--body" ]]; then
			((i++))
			echo -n "${!i}" > "$YAS_TEST_PR_BODY_FILE"
			exit 0
		fi
	done
	exit 0
elif [[ "$1" == "pr" && "$2" == "create" ]]; then
	# Simulate successful PR creation
	echo "https://github.com/test/test/pull/42"
	exit 0
fi
exit 0
`
	err := os.WriteFile(mockGH, []byte(mockGHScript), 0o755)
	assert.NilError(t, err)

	// Get path to existing mock script for git
	mockScript, err := filepath.Abs("testdata/mock-cmd.sh")
	assert.NilError(t, err)

	// Create symlink for git
	mockGit := filepath.Join(tmpDir, "git")
	err = os.Symlink(mockScript, mockGit)
	assert.NilError(t, err)

	// Find real git for fallback
	whichGitCmd := mustExecOutput("which", "git")
	realGit := strings.TrimSpace(whichGitCmd)

	// Create PR body file
	prBodyFile = filepath.Join(tmpDir, "pr-body.txt")
	err = os.WriteFile(prBodyFile, []byte(*prBody), 0o644)
	assert.NilError(t, err)

	// Set up environment
	t.Setenv("PATH", tmpDir+":"+os.Getenv("PATH"))
	t.Setenv("YAS_TEST_REAL_GIT", realGit)
	t.Setenv("YAS_TEST_PR_BODY_FILE", prBodyFile)

	if pr.ID != "" {
		t.Setenv("YAS_TEST_EXISTING_PR_ID", pr.ID)
	}

	if pr.State != "" {
		t.Setenv("YAS_TEST_PR_STATE", pr.State)
	}

	if pr.URL != "" {
		t.Setenv("YAS_TEST_PR_URL", pr.URL)
	}

	// Clean up any temp files from previous test runs
	files, _ := filepath.Glob("/tmp/yas-test-pr-created-*")
	for _, f := range files {
		assert.NilError(t, os.Remove(f))
	}

	cleanup = func() {
		// Read final body before cleanup
		bodyBytes, err := os.ReadFile(prBodyFile)
		if err == nil {
			*prBody = string(bodyBytes)
		}

		// Note: t.Setenv() automatically handles environment variable cleanup
		// Only need to clean up temp directory
		assert.NilError(t, os.RemoveAll(tmpDir))
	}

	return prBodyFile, cleanup
}
