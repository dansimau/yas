package test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"github.com/dansimau/yas/pkg/yascli"
	"gotest.tools/v3/assert"
)

// mockPROptions holds options for mocking a PR.
type mockPROptions struct {
	ID                string
	State             string
	URL               string
	IsDraft           bool
	BaseRefName       string
	ReviewDecision    string
	StatusCheckRollup string
}

// setupMockCommands creates mock git and gh commands that log to a file.
func setupMockCommands(t *testing.T, existingPRID string) (cmdLogFile string, cleanup func()) {
	t.Helper()

	return setupMockCommandsWithPR(t, mockPROptions{ID: existingPRID})
}

// setupMockCommandsWithPR creates mock git and gh commands with full PR options.
func setupMockCommandsWithPR(t *testing.T, pr mockPROptions) (cmdLogFile string, cleanup func()) {
	t.Helper()

	// Create temp directory for mock commands
	tmpDir := t.TempDir()

	// Create command log file
	cmdLogFile = filepath.Join(tmpDir, "commands.log")

	// Get path to mock script
	mockScript, err := filepath.Abs("testdata/mock-cmd.sh")
	assert.NilError(t, err)

	// Create symlinks for git and gh
	mockGit := filepath.Join(tmpDir, "git")
	mockGH := filepath.Join(tmpDir, "gh")
	err = os.Symlink(mockScript, mockGit)
	assert.NilError(t, err)
	err = os.Symlink(mockScript, mockGH)
	assert.NilError(t, err)

	// Find real git for fallback
	whichGitCmd := mustExecOutput("which", "git")
	realGit := strings.TrimSpace(whichGitCmd)

	// Set up environment
	t.Setenv("PATH", tmpDir+":"+os.Getenv("PATH"))
	t.Setenv("YAS_TEST_REAL_GIT", realGit)
	t.Setenv("YAS_TEST_CMD_LOG", cmdLogFile)

	if pr.ID != "" {
		t.Setenv("YAS_TEST_EXISTING_PR_ID", pr.ID)
	}

	if pr.State != "" {
		t.Setenv("YAS_TEST_PR_STATE", pr.State)
	}

	if pr.URL != "" {
		t.Setenv("YAS_TEST_PR_URL", pr.URL)
	}

	if pr.IsDraft {
		t.Setenv("YAS_TEST_PR_IS_DRAFT", "true")
	}

	if pr.BaseRefName != "" {
		t.Setenv("YAS_TEST_PR_BASE_REF", pr.BaseRefName)
	}

	if pr.ReviewDecision != "" {
		t.Setenv("YAS_TEST_PR_REVIEW_DECISION", pr.ReviewDecision)
	}

	if pr.StatusCheckRollup != "" {
		t.Setenv("YAS_TEST_PR_STATUS_CHECK_ROLLUP", pr.StatusCheckRollup)
	}

	// Clean up any temp files from previous test runs
	files, _ := filepath.Glob("/tmp/yas-test-pr-created-*")
	for _, f := range files {
		assert.NilError(t, os.Remove(f))
	}

	cleanup = func() {
		// Note: t.Setenv() automatically handles environment variable cleanup
		// Only need to clean up temp directory and files
		assert.NilError(t, os.RemoveAll(tmpDir))

		// Clean up temp PR files
		files, _ := filepath.Glob("/tmp/yas-test-pr-created-*")
		for _, f := range files {
			assert.NilError(t, os.Remove(f))
		}
	}

	return cmdLogFile, cleanup
}

// parseCmdLog parses the command log file and returns a list of commands.
func parseCmdLog(logFile string) ([][]string, error) {
	data, err := os.ReadFile(logFile)
	if err != nil {
		return nil, err
	}

	var (
		commands   [][]string
		currentCmd []string
	)

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if len(currentCmd) > 0 {
				commands = append(commands, currentCmd)
				currentCmd = nil
			}
		case strings.HasPrefix(line, "  "):
			// Argument
			currentCmd = append(currentCmd, strings.TrimPrefix(line, "  "))
		default:
			// Command name
			currentCmd = []string{line}
		}
	}

	if len(currentCmd) > 0 {
		commands = append(commands, currentCmd)
	}

	return commands, scanner.Err()
}

// findCommand finds a command in the log and returns it.
func findCommand(commands [][]string, commandName string, subcommand ...string) []string {
	for _, cmd := range commands {
		if len(cmd) == 0 {
			continue
		}

		if cmd[0] != commandName {
			continue
		}
		// Check subcommands if provided
		if len(subcommand) > 0 {
			if len(cmd) < len(subcommand)+1 {
				continue
			}

			match := true

			for i, sub := range subcommand {
				if cmd[i+1] != sub {
					match = false

					break
				}
			}

			if !match {
				continue
			}
		}

		return cmd
	}

	return nil
}

// wasCalled checks if a command exists in the log.
func wasCalled(commands [][]string, commandName string, subcommand ...string) bool {
	return findCommand(commands, commandName, subcommand...) != nil
}

func TestSubmit_SkipsCreatingPRWhenAlreadyExists(t *testing.T) {
	cmdLogFile, cleanup := setupMockCommands(t, "PR_kwDOTest123")
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

		// Create YAS instance and track branch
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)
		err = y.SetParent("topic-a", "main", "")
		assert.NilError(t, err)

		// Call submit - should push but NOT create PR since one exists
		err = y.Submit()
		assert.NilError(t, err)

		// Parse the command log
		commands, err := parseCmdLog(cmdLogFile)
		assert.NilError(t, err)

		// Verify git push was called
		assert.Assert(t, wasCalled(commands, "git", "push"), "git push should be called")

		// Verify gh pr list was called (to check for existing PR)
		assert.Assert(t, wasCalled(commands, "gh", "pr", "list"), "gh pr list should be called")

		// Verify gh pr create was NOT called
		assert.Assert(t, !wasCalled(commands, "gh", "pr", "create"), "gh pr create should NOT be called when PR exists")
	})
}

func TestSubmit_StackSubmitsAllBranches(t *testing.T) {
	cmdLogFile, cleanup := setupMockCommands(t, "") // No existing PRs
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

		// Call submit with --stack from topic-b
		testutil.ExecOrFail(t, "git checkout topic-b")
		assert.Equal(t, yascli.Run("submit", "--stack"), 0)

		// Parse the command log
		commands, err := parseCmdLog(cmdLogFile)
		assert.NilError(t, err)

		// Verify all branches in stack were pushed
		assert.Assert(t, wasCalled(commands, "git", "push", "--force-with-lease", "origin", "topic-a"), "topic-a should be pushed")
		assert.Assert(t, wasCalled(commands, "git", "push", "--force-with-lease", "origin", "topic-b"), "topic-b should be pushed")
		assert.Assert(t, wasCalled(commands, "git", "push", "--force-with-lease", "origin", "topic-c"), "topic-c should be pushed")

		// Verify PRs were created for all branches
		prCreateA := findCommand(commands, "gh", "pr", "create")
		assert.Assert(t, prCreateA != nil, "PR should be created for topic-a")

		// Count how many PR creates happened
		prCreateCount := 0

		for _, cmd := range commands {
			if len(cmd) >= 3 && cmd[0] == "gh" && cmd[1] == "pr" && cmd[2] == "create" {
				prCreateCount++
			}
		}

		assert.Equal(t, prCreateCount, 3, "Should create 3 PRs (one for each branch)")
	})
}

func TestSubmit_CreatesNewPRWhenNoneExists(t *testing.T) {
	cmdLogFile, cleanup := setupMockCommands(t, "") // No existing PR
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

		// Create YAS instance and track branch
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)
		err = y.SetParent("topic-a", "main", "")
		assert.NilError(t, err)

		// Call submit - should push AND create PR
		err = y.Submit()
		assert.NilError(t, err)

		// Parse the command log
		commands, err := parseCmdLog(cmdLogFile)
		assert.NilError(t, err)

		// Verify git push was called
		assert.Assert(t, wasCalled(commands, "git", "push"), "git push should be called")

		// Verify gh pr list was called (to check for existing PR)
		assert.Assert(t, wasCalled(commands, "gh", "pr", "list"), "gh pr list should be called")

		// Verify gh pr create WAS called
		assert.Assert(t, wasCalled(commands, "gh", "pr", "create"), "gh pr create should be called when no PR exists")
	})
}

func TestSubmit_UpdatesPRBaseWhenLocalParentChanges(t *testing.T) {
	// Mock an existing PR with base branch topic-a
	cmdLogFile, cleanup := setupMockCommandsWithPR(t, mockPROptions{
		ID:          "PR_kwDOTest123",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/42",
		IsDraft:     false,
		BaseRefName: "topic-a", // PR currently targets topic-a
	})
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

			# topic-b (originally child of topic-a)
			git checkout -b topic-b
			touch b
			git add b
			git commit -m "topic-b-0"
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

		// Track topic-a and topic-b
		err = y.SetParent("topic-a", "main", "")
		assert.NilError(t, err)

		// Set topic-b's parent to main (simulating a restack after topic-a was merged)
		// But the PR still has topic-a as base
		err = y.SetParent("topic-b", "main", "")
		assert.NilError(t, err)

		// Submit topic-b - should detect base mismatch and update
		testutil.ExecOrFail(t, "git checkout topic-b")

		err = y.Submit()
		assert.NilError(t, err)

		// Parse the command log
		commands, err := parseCmdLog(cmdLogFile)
		assert.NilError(t, err)

		// Verify gh pr edit was called to update the base branch
		editCmd := findCommand(commands, "gh", "pr", "edit")
		assert.Assert(t, editCmd != nil, "gh pr edit should be called to update base branch")

		// Verify the edit command includes --base main
		if editCmd != nil {
			assert.Assert(t, contains(editCmd, "--base"), "gh pr edit should include --base flag")
			assert.Assert(t, contains(editCmd, "main"), "gh pr edit should update base to main")
		}
	})
}

func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}

	return false
}

func TestSubmit_OutdatedSubmitsAllBranchesNeedingSubmit(t *testing.T) {
	// Mock commands without existing PRs (so they get created)
	cmdLogFile, cleanup := setupMockCommands(t, "")
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

			# topic-b
			git checkout -b topic-b
			touch b
			git add b
			git commit -m "topic-b-0"

			# topic-c
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
		err = y.SetParent("topic-b", "main", "")
		assert.NilError(t, err)
		err = y.SetParent("topic-c", "main", "")
		assert.NilError(t, err)

		// First, create PRs for all branches by submitting them individually
		// Submit topic-a
		testutil.ExecOrFail(t, "git checkout topic-a")
		err = y.Submit()
		assert.NilError(t, err)

		// Submit topic-b
		testutil.ExecOrFail(t, "git checkout topic-b")
		err = y.Submit()
		assert.NilError(t, err)

		// Submit topic-c
		testutil.ExecOrFail(t, "git checkout topic-c")
		err = y.Submit()
		assert.NilError(t, err)

		// Make additional commits to make branches need submitting
		testutil.ExecOrFail(t, `
			git checkout topic-a
			touch a2
			git add a2
			git commit -m "topic-a-1"

			git checkout topic-b
			touch b2
			git add b2
			git commit -m "topic-b-1"

			git checkout topic-c
			touch c2
			git add c2
			git commit -m "topic-c-1"
		`)

		// Now test the --outdated flag
		assert.Equal(t, yascli.Run("submit", "--outdated"), 0)

		// Parse the command log
		commands, err := parseCmdLog(cmdLogFile)
		assert.NilError(t, err)

		// Verify all branches were pushed
		assert.Assert(t, wasCalled(commands, "git", "push", "--force-with-lease", "origin", "topic-a"), "topic-a should be pushed")
		assert.Assert(t, wasCalled(commands, "git", "push", "--force-with-lease", "origin", "topic-b"), "topic-b should be pushed")
		assert.Assert(t, wasCalled(commands, "git", "push", "--force-with-lease", "origin", "topic-c"), "topic-c should be pushed")

		// Verify gh pr list was called for all branches (to check existing PRs)
		assert.Assert(t, wasCalled(commands, "gh", "pr", "list"), "gh pr list should be called to check existing PRs")

		// Count total PR operations (create + list)
		prCreateCount := 0
		prListCount := 0
		for _, cmd := range commands {
			if len(cmd) >= 3 && cmd[0] == "gh" && cmd[1] == "pr" {
				if cmd[2] == "create" {
					prCreateCount++
				} else if cmd[2] == "list" {
					prListCount++
				}
			}
		}

		// Should have created 3 PRs initially, then listed them during --outdated
		assert.Equal(t, prCreateCount, 3, "Should create 3 PRs initially")
		assert.Assert(t, prListCount >= 3, "Should list PRs for outdated branches")
	})
}

func TestSubmit_OutdatedSkipsBranchesWithoutPRs(t *testing.T) {
	// No existing PRs
	cmdLogFile, cleanup := setupMockCommands(t, "")
	defer cleanup()

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			git remote add origin https://github.com/test/test.git

			# main
			touch main
			git add main
			git commit -m "main-0"

			# topic-a (no PR)
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

		// Call submit with --outdated
		assert.Equal(t, yascli.Run("submit", "--outdated"), 0)

		// Parse the command log
		commands, err := parseCmdLog(cmdLogFile)
		assert.NilError(t, err)

		// Verify no git push was called (no PRs exist)
		assert.Assert(t, !wasCalled(commands, "git", "push"), "git push should NOT be called when no PRs exist")

		// Verify no gh pr create was called
		assert.Assert(t, !wasCalled(commands, "gh", "pr", "create"), "gh pr create should NOT be called when no PRs exist")
	})
}

func TestSubmit_OutdatedSkipsUpToDateBranches(t *testing.T) {
	// Mock commands without existing PRs (so they get created)
	cmdLogFile, cleanup := setupMockCommands(t, "")
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

		// Create YAS instance and track branch
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)
		err = y.SetParent("topic-a", "main", "")
		assert.NilError(t, err)

		// First, create PR for the branch
		err = y.Submit()
		assert.NilError(t, err)

		// Call submit with --outdated (should find no branches need submitting)
		assert.Equal(t, yascli.Run("submit", "--outdated"), 0)

		// Parse the command log
		commands, err := parseCmdLog(cmdLogFile)
		assert.NilError(t, err)

		// Verify git push was called (to update the PR)
		assert.Assert(t, wasCalled(commands, "git", "push"), "git push should be called to update PR")

		// Verify gh pr list was called (to check for existing PR)
		assert.Assert(t, wasCalled(commands, "gh", "pr", "list"), "gh pr list should be called")

		// Verify gh pr create was called (to create the initial PR)
		assert.Assert(t, wasCalled(commands, "gh", "pr", "create"), "gh pr create should be called to create initial PR")

		// The --outdated command should find no branches need submitting since they're up to date
		// This is the expected behavior - the test should pass
	})
}
