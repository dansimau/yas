package yas

import (
	"encoding/json"
	"os"
	"path"

	"github.com/dansimau/yas/pkg/fsutil"
)

// resolveRestackStatePath returns the first restack state path that exists,
// or the first path if none exist (for writing to the new location).
func (yas *YAS) resolveRestackStatePath() (string, error) {
	primaryWorktreePath, err := yas.git.PrimaryWorktreePath()
	if err != nil {
		return "", err
	}

	for _, filename := range restackStateFiles {
		fullPath := path.Join(primaryWorktreePath, filename)

		exists, err := fsutil.FileExists(fullPath)
		if err != nil {
			return "", err
		}

		if exists {
			return fullPath, nil
		}
	}

	// No file exists - use first (new) path for writing
	return path.Join(primaryWorktreePath, restackStateFiles[0]), nil
}

// saveRestackState saves the restack state to disk.
func (yas *YAS) saveRestackState(state *RestackState) error {
	filePath, err := yas.resolveRestackStatePath()
	if err != nil {
		return err
	}

	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	// Ensure the directory exists
	if err := os.MkdirAll(path.Dir(filePath), 0o755); err != nil {
		return err
	}

	return os.WriteFile(filePath, b, 0o644)
}

// loadRestackState loads the restack state from disk.
func (yas *YAS) loadRestackState() (*RestackState, error) {
	filePath, err := yas.resolveRestackStatePath()
	if err != nil {
		return nil, err
	}

	b, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	state := &RestackState{}
	if err := json.Unmarshal(b, state); err != nil {
		return nil, err
	}

	return state, nil
}

// deleteRestackState removes the restack state file.
func (yas *YAS) deleteRestackState() error {
	filePath, err := yas.resolveRestackStatePath()
	if err != nil {
		return err
	}

	err = os.Remove(filePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

// restackStateExists checks if a restack state file exists.
func (yas *YAS) restackStateExists() (bool, error) {
	filePath, err := yas.resolveRestackStatePath()
	if err != nil {
		return false, err
	}

	_, err = os.Stat(filePath)
	if err == nil {
		return true, nil
	}

	if os.IsNotExist(err) {
		return false, nil
	}

	return false, err
}

// Standalone functions for testing and backward compatibility

// resolveRestackStatePathStandalone returns the first restack state path that exists.
func resolveRestackStatePathStandalone(repoDir string) (string, error) {
	for _, filename := range restackStateFiles {
		fullPath := path.Join(repoDir, filename)

		exists, err := fsutil.FileExists(fullPath)
		if err != nil {
			return "", err
		}

		if exists {
			return fullPath, nil
		}
	}

	// No file exists - use first (new) path for writing
	return path.Join(repoDir, restackStateFiles[0]), nil
}

// SaveRestackState saves the restack state to disk (standalone version for testing).
func SaveRestackState(repoDir string, state *RestackState) error {
	filePath, err := resolveRestackStatePathStandalone(repoDir)
	if err != nil {
		return err
	}

	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	// Ensure the directory exists
	if err := os.MkdirAll(path.Dir(filePath), 0o755); err != nil {
		return err
	}

	return os.WriteFile(filePath, b, 0o644)
}

// LoadRestackState loads the restack state from disk (standalone version for testing).
func LoadRestackState(repoDir string) (*RestackState, error) {
	filePath, err := resolveRestackStatePathStandalone(repoDir)
	if err != nil {
		return nil, err
	}

	b, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	state := &RestackState{}
	if err := json.Unmarshal(b, state); err != nil {
		return nil, err
	}

	return state, nil
}

// DeleteRestackState removes the restack state file (standalone version for testing).
func DeleteRestackState(repoDir string) error {
	filePath, err := resolveRestackStatePathStandalone(repoDir)
	if err != nil {
		return err
	}

	err = os.Remove(filePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}
