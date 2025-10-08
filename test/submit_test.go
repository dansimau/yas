package test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

// setupMockCommands creates mock git and gh commands that log to a file
func setupMockCommands(t *testing.T, existingPRID string) (cmdLogFile string, cleanup func()) {
	t.Helper()

	// Create temp directory for mock commands
	tmpDir, err := os.MkdirTemp("", "yas-test-mock-*")
	assert.NilError(t, err)

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
	oldPath := os.Getenv("PATH")
	oldRealGit := os.Getenv("YAS_TEST_REAL_GIT")
	oldCmdLog := os.Getenv("YAS_TEST_CMD_LOG")
	oldExistingPR := os.Getenv("YAS_TEST_EXISTING_PR_ID")

	os.Setenv("PATH", tmpDir+":"+oldPath)
	os.Setenv("YAS_TEST_REAL_GIT", realGit)
	os.Setenv("YAS_TEST_CMD_LOG", cmdLogFile)
	if existingPRID != "" {
		os.Setenv("YAS_TEST_EXISTING_PR_ID", existingPRID)
	}

	cleanup = func() {
		os.Setenv("PATH", oldPath)
		os.Setenv("YAS_TEST_REAL_GIT", oldRealGit)
		os.Setenv("YAS_TEST_CMD_LOG", oldCmdLog)
		os.Setenv("YAS_TEST_EXISTING_PR_ID", oldExistingPR)
		os.RemoveAll(tmpDir)
	}

	return cmdLogFile, cleanup
}

// parseCmdLog parses the command log file and returns a list of commands
func parseCmdLog(logFile string) ([][]string, error) {
	data, err := os.ReadFile(logFile)
	if err != nil {
		return nil, err
	}

	var commands [][]string
	var currentCmd []string

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(currentCmd) > 0 {
				commands = append(commands, currentCmd)
				currentCmd = nil
			}
		} else if strings.HasPrefix(line, "  ") {
			// Argument
			currentCmd = append(currentCmd, strings.TrimPrefix(line, "  "))
		} else {
			// Command name
			currentCmd = []string{line}
		}
	}

	if len(currentCmd) > 0 {
		commands = append(commands, currentCmd)
	}

	return commands, scanner.Err()
}

// findCommand finds a command in the log that matches the given predicate
func findCommand(commands [][]string, predicate func([]string) bool) bool {
	for _, cmd := range commands {
		if predicate(cmd) {
			return true
		}
	}
	return false
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
		err = y.SetParent("topic-a", "main")
		assert.NilError(t, err)

		// Call submit - should push but NOT create PR since one exists
		err = y.Submit()
		assert.NilError(t, err)

		// Parse the command log
		commands, err := parseCmdLog(cmdLogFile)
		assert.NilError(t, err)

		// Verify git push was called
		foundPush := findCommand(commands, func(cmd []string) bool {
			return len(cmd) >= 2 && cmd[0] == "git" && cmd[1] == "push"
		})
		assert.Assert(t, foundPush, "git push should be called")

		// Verify gh pr list was called (to check for existing PR)
		foundPRList := findCommand(commands, func(cmd []string) bool {
			return len(cmd) >= 3 && cmd[0] == "gh" && cmd[1] == "pr" && cmd[2] == "list"
		})
		assert.Assert(t, foundPRList, "gh pr list should be called")

		// Verify gh pr create was NOT called
		foundPRCreate := findCommand(commands, func(cmd []string) bool {
			return len(cmd) >= 3 && cmd[0] == "gh" && cmd[1] == "pr" && cmd[2] == "create"
		})
		assert.Assert(t, !foundPRCreate, "gh pr create should NOT be called when PR exists")
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
		err = y.SetParent("topic-a", "main")
		assert.NilError(t, err)

		// Call submit - should push AND create PR
		err = y.Submit()
		assert.NilError(t, err)

		// Parse the command log
		commands, err := parseCmdLog(cmdLogFile)
		assert.NilError(t, err)

		// Verify git push was called
		foundPush := findCommand(commands, func(cmd []string) bool {
			return len(cmd) >= 2 && cmd[0] == "git" && cmd[1] == "push"
		})
		assert.Assert(t, foundPush, "git push should be called")

		// Verify gh pr list was called (to check for existing PR)
		foundPRList := findCommand(commands, func(cmd []string) bool {
			return len(cmd) >= 3 && cmd[0] == "gh" && cmd[1] == "pr" && cmd[2] == "list"
		})
		assert.Assert(t, foundPRList, "gh pr list should be called")

		// Verify gh pr create WAS called
		foundPRCreate := findCommand(commands, func(cmd []string) bool {
			return len(cmd) >= 3 && cmd[0] == "gh" && cmd[1] == "pr" && cmd[2] == "create"
		})
		assert.Assert(t, foundPRCreate, "gh pr create should be called when no PR exists")
	})
}
