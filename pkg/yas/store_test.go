package yas

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

func TestMigrateCreatedTimestamps(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	dbFilePath := filepath.Join(tmpDir, ".yasstate")

	// Create a database file with branches that have zero Created timestamps
	// (simulating data from before the Created field was added)
	initialData := yasData{
		Branches: &branchMap{
			data: map[string]BranchMetadata{
				"branch1": {
					Parent: "main",
				},
				"branch2": {
					Parent: "branch1",
				},
				"branch3": {
					Parent: "main",
				},
			},
		},
	}

	// Write the initial data to disk
	b, err := json.MarshalIndent(initialData, "", "  ")
	assert.NilError(t, err)
	err = os.WriteFile(dbFilePath, b, 0o644)
	assert.NilError(t, err)

	// Record the time before loading to verify timestamps are set
	timeBeforeLoad := time.Now()

	// Load the database - this should trigger the migration
	db, err := loadData(dbFilePath)
	assert.NilError(t, err)

	// Verify that all branches now have Created timestamps
	branch1 := db.Branches.Get("branch1")
	branch2 := db.Branches.Get("branch2")
	branch3 := db.Branches.Get("branch3")

	assert.Assert(t, !branch1.Created.IsZero(), "branch1 Created timestamp should be set")
	assert.Assert(t, !branch2.Created.IsZero(), "branch2 Created timestamp should be set")
	assert.Assert(t, !branch3.Created.IsZero(), "branch3 Created timestamp should be set")

	// Verify timestamps are reasonable (set during migration)
	assert.Assert(t, branch1.Created.After(timeBeforeLoad.Add(-time.Second)), "branch1 timestamp should be recent")
	assert.Assert(t, branch2.Created.After(timeBeforeLoad.Add(-time.Second)), "branch2 timestamp should be recent")
	assert.Assert(t, branch3.Created.After(timeBeforeLoad.Add(-time.Second)), "branch3 timestamp should be recent")

	// Store the timestamps for comparison
	timestamp1 := branch1.Created
	timestamp2 := branch2.Created
	timestamp3 := branch3.Created

	// Load the database again - this should NOT change the timestamps
	db2, err := loadData(dbFilePath)
	assert.NilError(t, err)

	branch1Again := db2.Branches.Get("branch1")
	branch2Again := db2.Branches.Get("branch2")
	branch3Again := db2.Branches.Get("branch3")

	// Verify timestamps are preserved exactly
	assert.Assert(t, branch1Again.Created.Equal(timestamp1), "branch1 timestamp should not change on reload")
	assert.Assert(t, branch2Again.Created.Equal(timestamp2), "branch2 timestamp should not change on reload")
	assert.Assert(t, branch3Again.Created.Equal(timestamp3), "branch3 timestamp should not change on reload")

	// Verify the timestamps were actually persisted to disk
	// Read the raw file and check it contains the timestamps
	rawData, err := os.ReadFile(dbFilePath)
	assert.NilError(t, err)

	var persistedData yasData

	err = json.Unmarshal(rawData, &persistedData)
	assert.NilError(t, err)

	assert.Assert(t, !persistedData.Branches.data["branch1"].Created.IsZero(), "branch1 timestamp should be persisted to disk")
	assert.Assert(t, !persistedData.Branches.data["branch2"].Created.IsZero(), "branch2 timestamp should be persisted to disk")
	assert.Assert(t, !persistedData.Branches.data["branch3"].Created.IsZero(), "branch3 timestamp should be persisted to disk")
}

func TestMigrateCreatedTimestamps_NoMigrationNeeded(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	dbFilePath := filepath.Join(tmpDir, ".yasstate")

	// Create a database file with branches that already have Created timestamps
	now := time.Now()
	initialData := yasData{
		Branches: &branchMap{
			data: map[string]BranchMetadata{
				"branch1": {
					Parent:  "main",
					Created: now.Add(-24 * time.Hour),
				},
				"branch2": {
					Parent:  "branch1",
					Created: now.Add(-12 * time.Hour),
				},
			},
		},
	}

	// Write the initial data to disk
	b, err := json.MarshalIndent(initialData, "", "  ")
	assert.NilError(t, err)
	err = os.WriteFile(dbFilePath, b, 0o644)
	assert.NilError(t, err)

	// Store the original file modification time
	fileInfoBefore, err := os.Stat(dbFilePath)
	assert.NilError(t, err)

	modTimeBefore := fileInfoBefore.ModTime()

	// Small delay to ensure any modification would change the timestamp
	time.Sleep(10 * time.Millisecond)

	// Load the database - migration should not be needed
	db, err := loadData(dbFilePath)
	assert.NilError(t, err)

	// Verify timestamps are unchanged
	branch1 := db.Branches.Get("branch1")
	branch2 := db.Branches.Get("branch2")

	assert.Assert(t, branch1.Created.Equal(now.Add(-24*time.Hour)), "branch1 timestamp should be unchanged")
	assert.Assert(t, branch2.Created.Equal(now.Add(-12*time.Hour)), "branch2 timestamp should be unchanged")

	// Verify the file was NOT modified (no needless save)
	fileInfoAfter, err := os.Stat(dbFilePath)
	assert.NilError(t, err)

	modTimeAfter := fileInfoAfter.ModTime()

	assert.Assert(t, modTimeBefore.Equal(modTimeAfter), "file should not be modified when no migration is needed")
}

func TestMigrateCreatedTimestamps_EmptyBranchName(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	dbFilePath := filepath.Join(tmpDir, ".yasstate")

	// Create a database file with an empty branch name (edge case)
	initialData := yasData{
		Branches: &branchMap{
			data: map[string]BranchMetadata{
				"": {
					Parent: "main",
				},
				"branch1": {
					Parent: "main",
				},
			},
		},
	}

	// Write the initial data to disk
	b, err := json.MarshalIndent(initialData, "", "  ")
	assert.NilError(t, err)
	err = os.WriteFile(dbFilePath, b, 0o644)
	assert.NilError(t, err)

	// Load the database
	db, err := loadData(dbFilePath)
	assert.NilError(t, err)

	// Verify that the empty branch name does NOT get a timestamp
	emptyBranch := db.Branches.Get("")
	assert.Assert(t, emptyBranch.Created.IsZero(), "empty branch name should not get a timestamp")

	// Verify that the normal branch does get a timestamp
	branch1 := db.Branches.Get("branch1")
	assert.Assert(t, !branch1.Created.IsZero(), "branch1 should get a timestamp")
}

func TestReload(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	dbFilePath := filepath.Join(tmpDir, ".yasstate")

	// Create a database file with initial data
	now := time.Now()
	initialData := yasData{
		Branches: &branchMap{
			data: map[string]BranchMetadata{
				"branch1": {
					Parent:  "main",
					Created: now,
				},
			},
		},
	}

	// Write the initial data to disk
	b, err := json.MarshalIndent(initialData, "", "  ")
	assert.NilError(t, err)
	err = os.WriteFile(dbFilePath, b, 0o644)
	assert.NilError(t, err)

	// Load the database
	db, err := loadData(dbFilePath)
	assert.NilError(t, err)

	// Verify initial state
	assert.Assert(t, db.Branches.Exists("branch1"), "branch1 should exist")
	assert.Assert(t, !db.Branches.Exists("branch2"), "branch2 should not exist yet")

	// Modify the file on disk (simulating external changes, e.g., from another process)
	updatedData := yasData{
		Branches: &branchMap{
			data: map[string]BranchMetadata{
				"branch1": {
					Parent:  "main",
					Created: now,
				},
				"branch2": {
					Parent:  "branch1",
					Created: now,
				},
			},
		},
	}

	b, err = json.MarshalIndent(updatedData, "", "  ")
	assert.NilError(t, err)
	err = os.WriteFile(dbFilePath, b, 0o644)
	assert.NilError(t, err)

	// Reload should pick up the new data
	err = db.Reload()
	assert.NilError(t, err)

	// Verify reloaded state
	assert.Assert(t, db.Branches.Exists("branch1"), "branch1 should still exist")
	assert.Assert(t, db.Branches.Exists("branch2"), "branch2 should now exist after reload")

	branch2 := db.Branches.Get("branch2")
	assert.Equal(t, "branch1", branch2.Parent)
}

func TestReload_FileDeleted(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	dbFilePath := filepath.Join(tmpDir, ".yasstate")

	// Create a database file with initial data
	now := time.Now()
	initialData := yasData{
		Branches: &branchMap{
			data: map[string]BranchMetadata{
				"branch1": {
					Parent:  "main",
					Created: now,
				},
			},
		},
	}

	// Write the initial data to disk
	b, err := json.MarshalIndent(initialData, "", "  ")
	assert.NilError(t, err)
	err = os.WriteFile(dbFilePath, b, 0o644)
	assert.NilError(t, err)

	// Load the database
	db, err := loadData(dbFilePath)
	assert.NilError(t, err)

	// Verify initial state
	assert.Assert(t, db.Branches.Exists("branch1"), "branch1 should exist")

	// Delete the file
	err = os.Remove(dbFilePath)
	assert.NilError(t, err)

	// Reload should reset to empty state
	err = db.Reload()
	assert.NilError(t, err)

	// Verify state is now empty
	assert.Assert(t, !db.Branches.Exists("branch1"), "branch1 should no longer exist after reload with deleted file")
}

func TestReload_BranchRemoved(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	dbFilePath := filepath.Join(tmpDir, ".yasstate")

	// Create a database file with initial data
	now := time.Now()
	initialData := yasData{
		Branches: &branchMap{
			data: map[string]BranchMetadata{
				"branch1": {
					Parent:  "main",
					Created: now,
				},
				"branch2": {
					Parent:  "branch1",
					Created: now,
				},
			},
		},
	}

	// Write the initial data to disk
	b, err := json.MarshalIndent(initialData, "", "  ")
	assert.NilError(t, err)
	err = os.WriteFile(dbFilePath, b, 0o644)
	assert.NilError(t, err)

	// Load the database
	db, err := loadData(dbFilePath)
	assert.NilError(t, err)

	// Verify initial state
	assert.Assert(t, db.Branches.Exists("branch1"), "branch1 should exist")
	assert.Assert(t, db.Branches.Exists("branch2"), "branch2 should exist")

	// Modify the file on disk to remove branch2
	updatedData := yasData{
		Branches: &branchMap{
			data: map[string]BranchMetadata{
				"branch1": {
					Parent:  "main",
					Created: now,
				},
			},
		},
	}

	b, err = json.MarshalIndent(updatedData, "", "  ")
	assert.NilError(t, err)
	err = os.WriteFile(dbFilePath, b, 0o644)
	assert.NilError(t, err)

	// Reload should pick up the change
	err = db.Reload()
	assert.NilError(t, err)

	// Verify branch2 is gone
	assert.Assert(t, db.Branches.Exists("branch1"), "branch1 should still exist")
	assert.Assert(t, !db.Branches.Exists("branch2"), "branch2 should be gone after reload")
}
