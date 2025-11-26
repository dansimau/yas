package test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

// TestConfigFileBackwardsCompatibility tests that config files in the old
// location (.git/yas.yaml) can still be read.
func TestConfigFileBackwardsCompatibility(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"
	`)

	// Write config to old location
	assert.NilError(t, os.MkdirAll(filepath.Join(tempDir, ".git"), 0o755))

	configContent := `trunkBranch: main
autoPrefixBranch: true
`
	assert.NilError(t, os.WriteFile(filepath.Join(tempDir, ".git/yas.yaml"), []byte(configContent), 0o644))

	// Verify old location file exists and new doesn't
	assert.Assert(t, assertFileExists(t, filepath.Join(tempDir, ".git/yas.yaml")))
	assert.Assert(t, !assertFileExists(t, filepath.Join(tempDir, ".yas/yas.yaml")))

	// Read config should work
	cfg, err := yas.ReadConfig(tempDir)
	assert.NilError(t, err)
	assert.Equal(t, cfg.TrunkBranch, "main")
	assert.Equal(t, cfg.AutoPrefixBranch, true)
}

// TestConfigFileStaysAtOldLocation tests that if config exists at old location,
// it continues to be used (no automatic migration).
func TestConfigFileStaysAtOldLocation(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"
	`)

	// Write config to old location
	assert.NilError(t, os.MkdirAll(filepath.Join(tempDir, ".git"), 0o755))

	configContent := `trunkBranch: main
autoPrefixBranch: true
`
	assert.NilError(t, os.WriteFile(filepath.Join(tempDir, ".git/yas.yaml"), []byte(configContent), 0o644))

	// Read and write config
	cfg, err := yas.ReadConfig(tempDir)
	assert.NilError(t, err)

	cfg.TrunkBranch = "master"

	_, err = yas.WriteConfig(*cfg)
	assert.NilError(t, err)

	// Old location should still be used
	assert.Assert(t, assertFileExists(t, filepath.Join(tempDir, ".git/yas.yaml")))

	// New location should NOT exist (no automatic migration)
	assert.Assert(t, !assertFileExists(t, filepath.Join(tempDir, ".yas/yas.yaml")))

	// Reading should use old location
	cfg2, err := yas.ReadConfig(tempDir)
	assert.NilError(t, err)
	assert.Equal(t, cfg2.TrunkBranch, "master")
}

// TestConfigFileNewLocation tests that new installations use new location.
func TestConfigFileNewLocation(t *testing.T) {
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
	`)

	// Initialize with new location
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())

	// Should create in new location
	assert.Assert(t, assertFileExists(t, filepath.Join(tempDir, ".yas/yas.yaml")))
	assert.Assert(t, !assertFileExists(t, filepath.Join(tempDir, ".git/yas.yaml")))

	// Should be readable
	cfg, err := yas.ReadConfig(tempDir)
	assert.NilError(t, err)
	assert.Equal(t, cfg.TrunkBranch, "main")
}

// TestStateFileBackwardsCompatibility tests that state files in the old
// location (.git/.yasstate) can still be read.
func TestStateFileBackwardsCompatibility(t *testing.T) {
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

		git checkout -b feature
		touch feature
		git add feature
		git commit -m "feature-0"
	`)

	// Set up config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())

	// Write state to old location
	stateContent := `{
  "branches": {
    "feature": {
      "Parent": "main"
    }
  }
}
`
	assert.NilError(t, os.WriteFile(filepath.Join(tempDir, ".git/.yasstate"), []byte(stateContent), 0o644))

	// Verify old location file exists and new doesn't
	assert.Assert(t, assertFileExists(t, filepath.Join(tempDir, ".git/.yasstate")))
	assert.Assert(t, !assertFileExists(t, filepath.Join(tempDir, ".yas/yas.state.json")))

	// Load YAS should work
	y, err := yas.NewFromRepository(tempDir)
	assert.NilError(t, err)

	// Should be able to read branch metadata
	meta := y.BranchMetadata("feature")
	assert.Equal(t, meta.Parent, "main")
}

// TestStateFileStaysAtOldLocation tests that if state exists at old location,
// it continues to be used (no automatic migration).
func TestStateFileStaysAtOldLocation(t *testing.T) {
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

		git checkout -b feature
		touch feature
		git add feature
		git commit -m "feature-0"
	`)

	// Set up config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())

	// Write state to old location
	stateContent := `{
  "branches": {
    "feature": {
      "Parent": "main"
    }
  }
}
`
	assert.NilError(t, os.WriteFile(filepath.Join(tempDir, ".git/.yasstate"), []byte(stateContent), 0o644))

	// Track a branch which will trigger a save
	assert.NilError(t, cli.Run("add", "feature", "--parent=main").Err())

	// Old location should still be used
	assert.Assert(t, assertFileExists(t, filepath.Join(tempDir, ".git/.yasstate")))

	// New location should NOT exist (no automatic migration)
	assert.Assert(t, !assertFileExists(t, filepath.Join(tempDir, ".yas/yas.state.json")))

	// Reading should use old location
	y, err := yas.NewFromRepository(tempDir)
	assert.NilError(t, err)

	meta := y.BranchMetadata("feature")
	assert.Equal(t, meta.Parent, "main")
}

// TestStateFileNewLocation tests that new installations use new location.
func TestStateFileNewLocation(t *testing.T) {
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

		git checkout -b feature
		touch feature
		git add feature
		git commit -m "feature-0"
	`)

	// Initialize with new location
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "feature", "--parent=main").Err())

	// Should create in new location
	assert.Assert(t, assertFileExists(t, filepath.Join(tempDir, ".yas/yas.state.json")))
	assert.Assert(t, !assertFileExists(t, filepath.Join(tempDir, ".git/.yasstate")))

	// Should be readable
	y, err := yas.NewFromRepository(tempDir)
	assert.NilError(t, err)

	meta := y.BranchMetadata("feature")
	assert.Equal(t, meta.Parent, "main")
}

// TestRestackStateFileBackwardsCompatibility tests that restack state files
// in the old location (.git/.yasrestack) can still be read.
func TestRestackStateFileBackwardsCompatibility(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"
	`)

	// Write restack state to old location
	restackContent := `{
  "starting_branch": "main",
  "current_branch": "feature",
  "current_parent": "main",
  "remaining_work": [],
  "rebased_branches": []
}
`
	assert.NilError(t, os.WriteFile(filepath.Join(tempDir, ".git/.yasrestack"), []byte(restackContent), 0o644))

	// Verify old location file exists and new doesn't
	assert.Assert(t, assertFileExists(t, filepath.Join(tempDir, ".git/.yasrestack")))
	assert.Assert(t, !assertFileExists(t, filepath.Join(tempDir, ".yas/yas.restack.json")))

	// Load restack state should work
	state, err := yas.LoadRestackState(tempDir)
	assert.NilError(t, err)
	assert.Equal(t, state.StartingBranch, "main")
	assert.Equal(t, state.CurrentBranch, "feature")
}

// TestRestackStateFileStaysAtOldLocation tests that if restack state exists at
// old location, it continues to be used (no automatic migration).
func TestRestackStateFileStaysAtOldLocation(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"
	`)

	// Write restack state to old location
	restackContent := `{
  "starting_branch": "main",
  "current_branch": "feature",
  "current_parent": "main",
  "remaining_work": [],
  "rebased_branches": []
}
`
	assert.NilError(t, os.WriteFile(filepath.Join(tempDir, ".git/.yasrestack"), []byte(restackContent), 0o644))

	// Save new state which should use old location
	newState := &yas.RestackState{
		StartingBranch:  "main",
		CurrentBranch:   "feature2",
		CurrentParent:   "main",
		RemainingWork:   [][2]string{},
		RebasedBranches: []string{},
	}
	err := yas.SaveRestackState(tempDir, newState)
	assert.NilError(t, err)

	// Old location should still be used
	assert.Assert(t, assertFileExists(t, filepath.Join(tempDir, ".git/.yasrestack")))

	// New location should NOT exist (no automatic migration)
	assert.Assert(t, !assertFileExists(t, filepath.Join(tempDir, ".yas/yas.restack.json")))

	// Reading should use old location
	state, err := yas.LoadRestackState(tempDir)
	assert.NilError(t, err)
	assert.Equal(t, state.CurrentBranch, "feature2")
}

// TestRestackStateFileNewLocation tests that new installations use new location.
func TestRestackStateFileNewLocation(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"
	`)

	// Save restack state
	state := &yas.RestackState{
		StartingBranch:  "main",
		CurrentBranch:   "feature",
		CurrentParent:   "main",
		RemainingWork:   [][2]string{},
		RebasedBranches: []string{},
	}
	err := yas.SaveRestackState(tempDir, state)
	assert.NilError(t, err)

	// Should create in new location
	assert.Assert(t, assertFileExists(t, filepath.Join(tempDir, ".yas/yas.restack.json")))
	assert.Assert(t, !assertFileExists(t, filepath.Join(tempDir, ".git/.yasrestack")))

	// Should be readable
	loadedState, err := yas.LoadRestackState(tempDir)
	assert.NilError(t, err)
	assert.Equal(t, loadedState.StartingBranch, "main")
	assert.Equal(t, loadedState.CurrentBranch, "feature")
}

// TestRestackStateFileExists tests backwards compatibility for RestackStateExists.
func TestRestackStateFileExists(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		touch main
		git add main
		git commit -m "main-0"
	`)

	// Initially no file exists
	assert.Assert(t, !assertRestackStateExists(t, tempDir))

	// Create in old location
	restackContent := `{}`
	assert.NilError(t, os.WriteFile(filepath.Join(tempDir, ".git/.yasrestack"), []byte(restackContent), 0o644))

	// Should detect old location
	assert.Assert(t, assertRestackStateExists(t, tempDir))

	// Delete old location
	assert.NilError(t, yas.DeleteRestackState(tempDir))
	assert.Assert(t, !assertRestackStateExists(t, tempDir))

	// Create in new location
	assert.NilError(t, os.MkdirAll(filepath.Join(tempDir, ".yas"), 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(tempDir, ".yas/yas.restack.json"), []byte(restackContent), 0o644))

	// Should detect new location
	assert.Assert(t, assertRestackStateExists(t, tempDir))

	// Delete new location
	assert.NilError(t, yas.DeleteRestackState(tempDir))
	assert.Assert(t, !assertRestackStateExists(t, tempDir))
}
