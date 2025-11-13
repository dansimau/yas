package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var ErrFileNotFound = errors.New("file not found")

func FileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}

	// Return false for "does not exist" errors
	if os.IsNotExist(err) {
		return false, nil
	}

	// Return false for "not a directory" errors (e.g., in git worktrees where
	// .git is a file, not a directory, so .git/yas.yaml cannot exist)
	if isNotDirError(err) {
		return false, nil
	}

	// For other errors (permissions, I/O errors, etc.), return the error
	// so the caller can handle it appropriately
	return false, err
}

// isNotDirError checks if the error is a "not a directory" error.
func isNotDirError(err error) bool {
	// On Unix systems, this is syscall.ENOTDIR
	// On Windows, this might be different
	// We check the error string as a fallback
	return err != nil && (errors.Is(err, os.ErrInvalid) ||
		strings.Contains(err.Error(), "not a directory"))
}

// searchPaths returns a list of paths from basePath upwards to the root ("/).
func searchPaths(basePath string) (paths []string) {
	root := basePath

	for root != "/" {
		paths = append(paths, root)
		root = filepath.Dir(root)
	}

	paths = append(paths, "/")

	return paths
}

func SearchParentsForPath(filename, searchPath string) (path string, err error) {
	for _, path := range searchPaths(searchPath) {
		f := filepath.Join(path, filename)

		exists, err := FileExists(f)
		if err != nil {
			return "", err
		}

		if exists {
			return f, nil
		}
	}

	return "", ErrFileNotFound
}

func SearchParentsForPathFromCwd(filename string) (path string, err error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	return SearchParentsForPath(filename, wd)
}
