package workyard

import (
	"errors"
	"fmt"
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

// FindSource returns the source directory that dir belongs to: the source of
// the workyard containing dir (see Find), else the nearest ancestor of dir
// holding a source's .workyard directory, else dir itself.
func FindSource(dir string) (string, error) {
	yard, err := Find(dir)
	if err == nil {
		return yard.Source, nil
	}

	if !errors.Is(err, ErrNotAWorkyard) {
		return "", err
	}

	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	for candidate := dir; ; {
		if info, err := os.Lstat(filepath.Join(candidate, workyardDir)); err == nil && info.IsDir() {
			return candidate, nil
		}

		parent := filepath.Dir(candidate)
		if parent == candidate {
			return dir, nil
		}

		candidate = parent
	}
}

// FindNamed opens the workyard called name, i.e. <yards-dir>/<name>, of the
// source dir belongs to (see FindSource).
func FindNamed(dir, name string) (*Yard, error) {
	if !filepath.IsLocal(name) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidName, name)
	}

	source, err := FindSource(dir)
	if err != nil {
		return nil, err
	}

	cfg, err := LoadConfig(source)
	if err != nil {
		return nil, err
	}

	yards, err := cfg.yardsDir(source)
	if err != nil {
		return nil, err
	}

	yard, err := Open(filepath.Join(yards, name))
	if errors.Is(err, ErrNotAWorkyard) {
		return nil, fmt.Errorf("%w: %s (looked in %s)", ErrNoSuchWorkyard, name, yards)
	}

	return yard, err
}
