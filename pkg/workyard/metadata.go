package workyard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func metadataPath(root string) string {
	return filepath.Join(root, workyardDir, metadataFile)
}

// writeMetadata writes the metadata file atomically (temp file + rename).
func writeMetadata(root string, meta Metadata) error {
	dir := filepath.Join(root, workyardDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}

	f, err := os.CreateTemp(dir, metadataFile+".*.tmp")
	if err != nil {
		return err
	}

	tmp := f.Name()

	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)

		return err
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)

		return err
	}

	if err := os.Rename(tmp, metadataPath(root)); err != nil {
		_ = os.Remove(tmp)

		return err
	}

	return nil
}

func readMetadata(root string) (Metadata, error) {
	var meta Metadata

	b, err := os.ReadFile(metadataPath(root))
	if err != nil {
		return meta, err
	}

	if err := json.Unmarshal(b, &meta); err != nil {
		return meta, fmt.Errorf("invalid %s: %w", metadataPath(root), err)
	}

	if meta.Version > MetadataVersion {
		return meta, fmt.Errorf("%s: unsupported version %d (upgrade workyard)", metadataPath(root), meta.Version)
	}

	return meta, nil
}

// Open loads the workyard rooted at root.
func Open(root string) (*Yard, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	meta, err := readMetadata(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNotAWorkyard, root)
		}

		return nil, err
	}

	return &Yard{Root: root, Meta: meta}, nil
}

// RepoDir returns the absolute path of a repository inside the yard.
func (y *Yard) RepoDir(repo Repo) string {
	return filepath.Join(y.Root, repo.Path)
}
