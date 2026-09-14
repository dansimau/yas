package workyard

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
)

// errCloneUnsupported is returned by a cloner on platforms or filesystems that
// cannot clone.
var errCloneUnsupported = errors.New("cloning is not supported")

// cloner makes copy-on-write clones. The real implementation is per platform;
// tests substitute their own.
type cloner interface {
	// CloneTree clones the directory tree (or single file) at src to dst, which
	// must not exist. It is all-or-nothing.
	CloneTree(src, dst string) error
}

// copier copies files and directory trees, cloning them where possible.
type copier struct {
	cloner cloner
	mode   CopyMode
	// cloneDisabled is set once cloning has failed for a reason that will
	// affect every further attempt (different volume, unsupported filesystem).
	cloneDisabled atomic.Bool
	warnOnce      sync.Once
	warn          func(string)

	cloned  atomic.Int64
	copied  atomic.Int64
	skipped atomic.Int64
}

func newCopier(mode CopyMode, warn func(string)) *copier {
	c := &copier{cloner: newCloner(), mode: mode, warn: warn}
	if mode == CopyPlain {
		c.cloneDisabled.Store(true)
	}

	return c
}

// tryClone attempts to clone src to dst. It reports whether the clone was
// made; when it returns (false, nil) the caller should copy instead.
func (c *copier) tryClone(src, dst string) (bool, error) {
	if c.cloneDisabled.Load() {
		return false, nil
	}

	err := c.cloner.CloneTree(src, dst)
	if err == nil {
		c.cloned.Add(1)

		return true, nil
	}

	if errors.Is(err, fs.ErrExist) || errors.Is(err, syscall.EEXIST) {
		// Cloning is all-or-nothing, so anything already at dst was put there
		// by someone else.
		return false, fmt.Errorf("clone %s: %w", dst, err)
	}

	if c.mode == CopyClone {
		return false, fmt.Errorf("clone %s: %w", dst, err)
	}

	if errors.Is(err, syscall.EXDEV) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, errCloneUnsupported) {
		c.cloneDisabled.Store(true)
		c.warnOnce.Do(func() {
			if c.warn != nil {
				c.warn("target cannot be cloned from source (different volume or unsupported filesystem); copying instead")
			}
		})
	}

	return false, nil
}

// CopyTree copies the directory at src (with Lstat info) to dst, cloning it
// when possible.
func (c *copier) CopyTree(src, dst string, info fs.FileInfo) error {
	cloned, err := c.tryClone(src, dst)
	if err != nil || cloned {
		return err
	}

	c.copied.Add(1)

	return c.copyDir(src, dst, info)
}

// copyDir recursively copies a directory.
func (c *copier) copyDir(src, dst string, info fs.FileInfo) error {
	// Create writable so children can be added; the real mode is applied
	// last, after the children, along with the modification time.
	if err := os.Mkdir(dst, info.Mode().Perm()|0o700); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, e := range entries {
		childInfo, err := e.Info()
		if err != nil {
			return err
		}

		childSrc := filepath.Join(src, e.Name())
		childDst := filepath.Join(dst, e.Name())

		if childInfo.IsDir() {
			err = c.copyDir(childSrc, childDst, childInfo)
		} else {
			err = c.copyChild(childSrc, childDst, childInfo)
		}

		if err != nil {
			return err
		}
	}

	return c.finishDir(dst, info)
}

// finishDir applies the source directory's mode and modification time to dst.
func (c *copier) finishDir(dst string, info fs.FileInfo) error {
	if err := os.Chmod(dst, info.Mode().Perm()); err != nil {
		return err
	}

	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

// CopyEntry copies a single non-directory entry that is a unit of its own
// (cloning regular files when possible).
func (c *copier) CopyEntry(src, dst string, info fs.FileInfo) error {
	if info.Mode().IsRegular() {
		return c.copyFile(src, dst, info)
	}

	return c.copyChild(src, dst, info)
}

// copyChild copies a non-directory entry inside a tree that is already being
// copied, so it is neither cloned nor counted.
func (c *copier) copyChild(src, dst string, info fs.FileInfo) error {
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}

		return os.Symlink(target, dst)
	case info.Mode().IsRegular():
		return c.copyFileData(src, dst, info)
	default:
		c.skipped.Add(1)

		if c.warn != nil {
			c.warn(fmt.Sprintf("skipping %s: not a regular file, directory or symlink", src))
		}

		return nil
	}
}

// copyFile copies one regular file as a unit: cloned when possible, copied
// otherwise.
func (c *copier) copyFile(src, dst string, info fs.FileInfo) error {
	cloned, err := c.tryClone(src, dst)
	if err != nil || cloned {
		return err
	}

	c.copied.Add(1)

	return c.copyFileData(src, dst, info)
}

// copyFileData copies the contents, mode and mtime of a regular file.
func (c *copier) copyFileData(src, dst string, info fs.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm()|0o600)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()

		return fmt.Errorf("copy %s: %w", src, err)
	}

	if err := out.Close(); err != nil {
		return err
	}

	if err := os.Chmod(dst, info.Mode().Perm()); err != nil {
		return err
	}

	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}
