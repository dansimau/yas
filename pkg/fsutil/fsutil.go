package fsutil

import (
	"errors"
	"os"
	"path/filepath"
)

var ErrFileNotFound = errors.New("file not found")

func FileExists(path string) bool {
	_, err := os.Stat(path)

	return !os.IsNotExist(err)
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
		if FileExists(f) {
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
