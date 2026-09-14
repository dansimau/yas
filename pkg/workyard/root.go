package workyard

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/dansimau/yas/pkg/fsutil"
)

// RootEnvVar names the environment variable that overrides root discovery.
const RootEnvVar = "WORKYARD_ROOT"

// findRoot walks up from dir looking for a workyard metadata file and returns
// the yard root, or "" when dir is not inside a workyard.
func findRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	found, err := fsutil.SearchParentsForPath(filepath.Join(workyardDir, metadataFile), dir)
	if err != nil {
		if errors.Is(err, fsutil.ErrFileNotFound) {
			return "", nil
		}

		return "", err
	}

	return filepath.Dir(filepath.Dir(found)), nil
}

// Find locates the workyard containing dir: WORKYARD_ROOT when set, otherwise
// the nearest ancestor of dir holding .workyard/metadata.json.
func Find(dir string) (*Yard, error) {
	if root := os.Getenv(RootEnvVar); root != "" {
		return Open(root)
	}

	root, err := findRoot(dir)
	if err != nil {
		return nil, err
	}

	if root == "" {
		return nil, ErrNotAWorkyard
	}

	return Open(root)
}
