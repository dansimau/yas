package test

import (
	"os"
	"testing"

	"github.com/dansimau/yas/pkg/fsutil"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"github.com/dansimau/yas/pkg/yascli"
	"gotest.tools/v3/assert"
)

// TestConfigFileBackwardsCompatibility tests that config files in the old
// location (.git/yas.yaml) can still be read
func TestConfigFileBackwardsCompatibility(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"
		`)

		// Write config to old location
		assert.NilError(t, os.MkdirAll(".git", 0o755))
		configContent := `trunkBranch: main
autoPrefixBranch: true
`
		assert.NilError(t, os.WriteFile(".git/yas.yaml", []byte(configContent), 0o644))

		// Verify old location file exists and new doesn't
		assert.Assert(t, fsutil.FileExists(".git/yas.yaml"))
		assert.Assert(t, !fsutil.FileExists(".yas/yas.yaml"))

		// Read config should work
		cfg, err := yas.ReadConfig(".")
		assert.NilError(t, err)
		assert.Equal(t, cfg.TrunkBranch, "main")
		assert.Equal(t, cfg.AutoPrefixBranch, true)
	})
}

// TestConfigFileStaysAtOldLocation tests that if config exists at old location,
// it continues to be used (no automatic migration)
func TestConfigFileStaysAtOldLocation(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"
		`)

		// Write config to old location
		assert.NilError(t, os.MkdirAll(".git", 0o755))
		configContent := `trunkBranch: main
autoPrefixBranch: true
`
		assert.NilError(t, os.WriteFile(".git/yas.yaml", []byte(configContent), 0o644))

		// Read and write config
		cfg, err := yas.ReadConfig(".")
		assert.NilError(t, err)
		cfg.TrunkBranch = "master"

		_, err = yas.WriteConfig(*cfg)
		assert.NilError(t, err)

		// Old location should still be used
		assert.Assert(t, fsutil.FileExists(".git/yas.yaml"))

		// New location should NOT exist (no automatic migration)
		assert.Assert(t, !fsutil.FileExists(".yas/yas.yaml"))

		// Reading should use old location
		cfg2, err := yas.ReadConfig(".")
		assert.NilError(t, err)
		assert.Equal(t, cfg2.TrunkBranch, "master")
	})
}

// TestConfigFileNewLocation tests that new installations use new location
func TestConfigFileNewLocation(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"
		`)

		// Initialize with new location
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)

		// Should create in new location
		assert.Assert(t, fsutil.FileExists(".yas/yas.yaml"))
		assert.Assert(t, !fsutil.FileExists(".git/yas.yaml"))

		// Should be readable
		cfg, err := yas.ReadConfig(".")
		assert.NilError(t, err)
		assert.Equal(t, cfg.TrunkBranch, "main")
	})
}

// TestStateFileBackwardsCompatibility tests that state files in the old
// location (.git/.yasstate) can still be read
func TestStateFileBackwardsCompatibility(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
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
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)

		// Write state to old location
		stateContent := `{
  "branches": {
    "feature": {
      "Parent": "main"
    }
  }
}
`
		assert.NilError(t, os.WriteFile(".git/.yasstate", []byte(stateContent), 0o644))

		// Verify old location file exists and new doesn't
		assert.Assert(t, fsutil.FileExists(".git/.yasstate"))
		assert.Assert(t, !fsutil.FileExists(".yas/yas.state.json"))

		// Load YAS should work
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)

		// Should be able to read branch metadata
		meta := y.BranchMetadata("feature")
		assert.Equal(t, meta.Parent, "main")
	})
}

// TestStateFileStaysAtOldLocation tests that if state exists at old location,
// it continues to be used (no automatic migration)
func TestStateFileStaysAtOldLocation(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
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
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)

		// Write state to old location
		stateContent := `{
  "branches": {
    "feature": {
      "Parent": "main"
    }
  }
}
`
		assert.NilError(t, os.WriteFile(".git/.yasstate", []byte(stateContent), 0o644))

		// Track a branch which will trigger a save
		assert.Equal(t, yascli.Run("add", "feature", "--parent=main"), 0)

		// Old location should still be used
		assert.Assert(t, fsutil.FileExists(".git/.yasstate"))

		// New location should NOT exist (no automatic migration)
		assert.Assert(t, !fsutil.FileExists(".yas/yas.state.json"))

		// Reading should use old location
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)
		meta := y.BranchMetadata("feature")
		assert.Equal(t, meta.Parent, "main")
	})
}

// TestStateFileNewLocation tests that new installations use new location
func TestStateFileNewLocation(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
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
		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "feature", "--parent=main"), 0)

		// Should create in new location
		assert.Assert(t, fsutil.FileExists(".yas/yas.state.json"))
		assert.Assert(t, !fsutil.FileExists(".git/.yasstate"))

		// Should be readable
		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)
		meta := y.BranchMetadata("feature")
		assert.Equal(t, meta.Parent, "main")
	})
}

// TestRestackStateFileBackwardsCompatibility tests that restack state files
// in the old location (.git/.yasrestack) can still be read
func TestRestackStateFileBackwardsCompatibility(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
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
		assert.NilError(t, os.WriteFile(".git/.yasrestack", []byte(restackContent), 0o644))

		// Verify old location file exists and new doesn't
		assert.Assert(t, fsutil.FileExists(".git/.yasrestack"))
		assert.Assert(t, !fsutil.FileExists(".yas/yas.restack.json"))

		// Load restack state should work
		state, err := yas.LoadRestackState(".")
		assert.NilError(t, err)
		assert.Equal(t, state.StartingBranch, "main")
		assert.Equal(t, state.CurrentBranch, "feature")
	})
}

// TestRestackStateFileStaysAtOldLocation tests that if restack state exists at
// old location, it continues to be used (no automatic migration)
func TestRestackStateFileStaysAtOldLocation(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
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
		assert.NilError(t, os.WriteFile(".git/.yasrestack", []byte(restackContent), 0o644))

		// Save new state which should use old location
		newState := &yas.RestackState{
			StartingBranch:  "main",
			CurrentBranch:   "feature2",
			CurrentParent:   "main",
			RemainingWork:   [][2]string{},
			RebasedBranches: []string{},
		}
		err := yas.SaveRestackState(".", newState)
		assert.NilError(t, err)

		// Old location should still be used
		assert.Assert(t, fsutil.FileExists(".git/.yasrestack"))

		// New location should NOT exist (no automatic migration)
		assert.Assert(t, !fsutil.FileExists(".yas/yas.restack.json"))

		// Reading should use old location
		state, err := yas.LoadRestackState(".")
		assert.NilError(t, err)
		assert.Equal(t, state.CurrentBranch, "feature2")
	})
}

// TestRestackStateFileNewLocation tests that new installations use new location
func TestRestackStateFileNewLocation(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
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
		err := yas.SaveRestackState(".", state)
		assert.NilError(t, err)

		// Should create in new location
		assert.Assert(t, fsutil.FileExists(".yas/yas.restack.json"))
		assert.Assert(t, !fsutil.FileExists(".git/.yasrestack"))

		// Should be readable
		loadedState, err := yas.LoadRestackState(".")
		assert.NilError(t, err)
		assert.Equal(t, loadedState.StartingBranch, "main")
		assert.Equal(t, loadedState.CurrentBranch, "feature")
	})
}

// TestRestackStateFileExists tests backwards compatibility for RestackStateExists
func TestRestackStateFileExists(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
			git init --initial-branch=main
			touch main
			git add main
			git commit -m "main-0"
		`)

		// Initially no file exists
		assert.Assert(t, !yas.RestackStateExists("."))

		// Create in old location
		restackContent := `{}`
		assert.NilError(t, os.WriteFile(".git/.yasrestack", []byte(restackContent), 0o644))

		// Should detect old location
		assert.Assert(t, yas.RestackStateExists("."))

		// Delete old location
		assert.NilError(t, yas.DeleteRestackState("."))
		assert.Assert(t, !yas.RestackStateExists("."))

		// Create in new location
		assert.NilError(t, os.MkdirAll(".yas", 0o755))
		assert.NilError(t, os.WriteFile(".yas/yas.restack.json", []byte(restackContent), 0o644))

		// Should detect new location
		assert.Assert(t, yas.RestackStateExists("."))

		// Delete new location
		assert.NilError(t, yas.DeleteRestackState("."))
		assert.Assert(t, !yas.RestackStateExists("."))
	})
}
