package workyard

import (
	"bufio"
	"crypto/sha1" //nolint:gosec // not used for security, just a stable id
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// workyardDir is the source's .workyard directory (config and yard
	// metadata) and the name of the pointer file at a yard's root.
	workyardDir = ".workyard"
	configFile  = "config.yaml"
	yardsDir    = "yards"
)

// yardID derives the id under which a yard's metadata is stored in its source
// from the yard's real path.
func yardID(target string) string {
	sum := sha1.Sum([]byte(target)) //nolint:gosec // see import

	return hex.EncodeToString(sum[:])[:12]
}

// pointerPath is the .workyard file at a yard's root.
func pointerPath(root string) string {
	return filepath.Join(root, workyardDir)
}

// isPointer reports whether dir holds a .workyard pointer file (as opposed to
// a source's .workyard directory, or nothing).
func isPointer(dir string) bool {
	info, err := os.Lstat(pointerPath(dir))

	return err == nil && info.Mode().IsRegular()
}

// writePointer writes the .workyard pointer file, which (like a worktree's
// .git file) only says where the yard's source and metadata live.
func writePointer(root, source, id string) error {
	content := fmt.Sprintf("workyard: %s\nid: %s\n", source, id)

	return os.WriteFile(pointerPath(root), []byte(content), 0o644)
}

// readPointer returns the source and id recorded in a yard's .workyard file.
func readPointer(root string) (source string, id string, err error) {
	f, err := os.Open(pointerPath(root))
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", "", err
	}

	if !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("%w: %s is not a workyard pointer", ErrNotAWorkyard, pointerPath(root))
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), ": ")
		if !found {
			continue
		}

		switch key {
		case "workyard":
			source = value
		case "id":
			id = value
		}
	}

	if err := scanner.Err(); err != nil {
		return "", "", err
	}

	if source == "" || id == "" {
		return "", "", fmt.Errorf("invalid workyard pointer %s", pointerPath(root))
	}

	return source, id, nil
}

func metadataPath(source, id string) string {
	return filepath.Join(source, workyardDir, yardsDir, id+".json")
}

// writeMetadata writes a yard's metadata into its source atomically (temp file
// + rename).
func writeMetadata(meta Metadata) error {
	dir := filepath.Dir(metadataPath(meta.Source, meta.ID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}

	f, err := os.CreateTemp(dir, meta.ID+".*.tmp")
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

	if err := os.Rename(tmp, metadataPath(meta.Source, meta.ID)); err != nil {
		_ = os.Remove(tmp)

		return err
	}

	return nil
}

func readMetadata(source, id string) (Metadata, error) {
	var meta Metadata

	b, err := os.ReadFile(metadataPath(source, id))
	if err != nil {
		return meta, err
	}

	if err := json.Unmarshal(b, &meta); err != nil {
		return meta, fmt.Errorf("invalid %s: %w", metadataPath(source, id), err)
	}

	if meta.Version > MetadataVersion {
		return meta, fmt.Errorf("%s: unsupported version %d (upgrade workyard)", metadataPath(source, id), meta.Version)
	}

	return meta, nil
}

// removeMetadata deletes a yard's metadata from its source, along with the
// yards directory (and the .workyard directory) when they are left empty.
func removeMetadata(source, id string) error {
	if err := os.Remove(metadataPath(source, id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	// Best effort: these fail when non-empty, which is fine.
	_ = os.Remove(filepath.Join(source, workyardDir, yardsDir))
	_ = os.Remove(filepath.Join(source, workyardDir))

	return nil
}

// Open loads the workyard rooted at root. When the source directory is gone
// it returns a *SourceMissingError.
func Open(root string) (*Yard, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	source, id, err := readPointer(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNotAWorkyard, root)
		}

		return nil, err
	}

	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		return nil, &SourceMissingError{Root: root, Source: source}
	}

	meta, err := readMetadata(source, id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("metadata for workyard %s not found in %s", root, filepath.Join(source, workyardDir, yardsDir))
		}

		return nil, err
	}

	return &Yard{Root: root, Source: source, ID: id, Meta: meta}, nil
}

// RepoDir returns the absolute path of a repository inside the yard.
func (y *Yard) RepoDir(repo Repo) string {
	return filepath.Join(y.Root, repo.Path)
}
