package workyard

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"gotest.tools/v3/assert"
)

func TestRemoveAll(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tree := filepath.Join(dir, "tree")
	outside := filepath.Join(dir, "outside")
	assert.NilError(t, os.MkdirAll(filepath.Join(outside, "sub"), 0o755))

	// Enough directories to keep every worker busy.
	for i := range 40 {
		sub := filepath.Join(tree, "many", strconv.Itoa(i), "deeper")
		assert.NilError(t, os.MkdirAll(sub, 0o755))

		for j := range 20 {
			assert.NilError(t, os.WriteFile(filepath.Join(sub, strconv.Itoa(j)), []byte("x"), 0o644))
		}
	}

	// Read-only files, read-only directories (whose entries cannot be
	// unlinked until they are writable again), a directory that cannot even
	// be listed, and a symlink to a directory that must be left alone.
	assert.NilError(t, os.MkdirAll(filepath.Join(tree, "a", "b", "c"), 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(tree, "a", "b", "c", "f"), []byte("x"), 0o444))
	assert.NilError(t, os.WriteFile(filepath.Join(tree, "a", "f"), []byte("x"), 0o644))
	assert.NilError(t, os.Symlink(outside, filepath.Join(tree, "a", "link")))
	assert.NilError(t, os.Chmod(filepath.Join(tree, "a", "b", "c"), 0o555))
	assert.NilError(t, os.Chmod(filepath.Join(tree, "a", "b"), 0o555))
	assert.NilError(t, os.MkdirAll(filepath.Join(tree, "d"), 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(tree, "d", "f"), nil, 0o644))
	assert.NilError(t, os.Chmod(filepath.Join(tree, "d"), 0o000))

	t.Cleanup(func() {
		for _, p := range []string{"a/b/c", "a/b", "d"} {
			_ = os.Chmod(filepath.Join(tree, p), 0o755)
		}
	})

	assert.NilError(t, removeAll(tree))

	_, err := os.Lstat(tree)
	assert.Assert(t, errors.Is(err, os.ErrNotExist), "tree should be gone: %v", err)

	_, err = os.Stat(filepath.Join(outside, "sub"))
	assert.NilError(t, err, "the symlink's target must survive")

	// Missing paths and plain files are fine too.
	assert.NilError(t, removeAll(tree))

	file := filepath.Join(dir, "file")
	assert.NilError(t, os.WriteFile(file, nil, 0o644))
	assert.NilError(t, removeAll(file))

	_, err = os.Lstat(file)
	assert.Assert(t, errors.Is(err, os.ErrNotExist))
}
