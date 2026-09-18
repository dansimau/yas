package workyard

import (
	"os"
	"path/filepath"
)

// RootEnvVar names the environment variable that overrides root discovery.
const RootEnvVar = "WORKYARD_ROOT"

// findRoot walks up from dir looking for a .workyard pointer file and returns
// the yard root, or "" when dir is not inside a workyard. A source's .workyard
// directory does not count.
func findRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	for {
		if isPointer(dir) {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}

		dir = parent
	}
}

// Find locates the workyard containing dir: WORKYARD_ROOT when set, otherwise
// the nearest ancestor of dir holding a .workyard pointer file.
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
