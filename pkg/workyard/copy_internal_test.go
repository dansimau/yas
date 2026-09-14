package workyard

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"gotest.tools/v3/assert"
)

// fakeCloner returns a scripted error per call and records the calls.
type fakeCloner struct {
	errs  []error
	calls int
}

func (f *fakeCloner) CloneTree(_, _ string) error {
	f.calls++

	if f.calls > len(f.errs) {
		return nil
	}

	return f.errs[f.calls-1]
}

func TestCopierCloneFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		mode         CopyMode
		cloneErr     error
		wantCloned   bool
		wantErr      bool
		wantDisabled bool
		wantWarning  bool
	}{
		{name: "clone succeeds", mode: CopyAuto, cloneErr: nil, wantCloned: true},
		{name: "EXDEV falls back and disables cloning", mode: CopyAuto, cloneErr: syscall.EXDEV, wantDisabled: true, wantWarning: true},
		{name: "ENOTSUP falls back and disables cloning", mode: CopyAuto, cloneErr: syscall.ENOTSUP, wantDisabled: true, wantWarning: true},
		{name: "unsupported platform falls back and disables cloning", mode: CopyAuto, cloneErr: errCloneUnsupported, wantDisabled: true, wantWarning: true},
		{name: "other error falls back without disabling", mode: CopyAuto, cloneErr: syscall.EIO},
		{name: "EEXIST is a hard error", mode: CopyAuto, cloneErr: syscall.EEXIST, wantErr: true},
		{name: "clone mode does not fall back", mode: CopyClone, cloneErr: syscall.EXDEV, wantErr: true},
		{name: "plain mode never clones", mode: CopyPlain, cloneErr: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			warnings := 0
			fake := &fakeCloner{errs: []error{tt.cloneErr}}
			c := newCopier(tt.mode, func(string) { warnings++ })
			c.cloner = fake

			cloned, err := c.tryClone("src", "dst")

			assert.Equal(t, err != nil, tt.wantErr, "err: %v", err)
			assert.Equal(t, cloned, tt.wantCloned)
			assert.Equal(t, c.cloneDisabled.Load(), tt.wantDisabled || tt.mode == CopyPlain)
			assert.Equal(t, warnings > 0, tt.wantWarning)
			assert.Equal(t, fake.calls, boolToInt(tt.mode != CopyPlain), "plain mode must not call the cloner")

			if tt.wantDisabled {
				// Once disabled, no further clone attempts are made and no
				// further warnings are printed.
				cloned, err = c.tryClone("src2", "dst2")
				assert.NilError(t, err)
				assert.Assert(t, !cloned)
				assert.Equal(t, fake.calls, 1)
				assert.Equal(t, warnings, 1)
			}
		})
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}

	return 0
}

func TestCopierPlainCopyPreservesTree(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "dst")

	assert.NilError(t, os.MkdirAll(filepath.Join(src, "dir", "inner"), 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(src, "dir", "inner", "file"), []byte("content"), 0o640))
	assert.NilError(t, os.WriteFile(filepath.Join(src, "dir", "exec"), []byte("#!/bin/sh\n"), 0o755))
	assert.NilError(t, os.Symlink("inner/file", filepath.Join(src, "dir", "link")))
	assert.NilError(t, os.Chmod(filepath.Join(src, "dir", "inner"), 0o555))

	t.Cleanup(func() {
		_ = os.Chmod(filepath.Join(src, "dir", "inner"), 0o755)
		_ = os.Chmod(filepath.Join(dst, "inner"), 0o755)
	})

	info, err := os.Lstat(filepath.Join(src, "dir"))
	assert.NilError(t, err)

	c := newCopier(CopyPlain, nil)
	assert.NilError(t, c.CopyTree(filepath.Join(src, "dir"), dst, info))
	assert.Equal(t, int(c.copied.Load()), 1)
	assert.Equal(t, int(c.cloned.Load()), 0)

	content, err := os.ReadFile(filepath.Join(dst, "inner", "file"))
	assert.NilError(t, err)
	assert.Equal(t, string(content), "content")

	fileInfo, err := os.Stat(filepath.Join(dst, "inner", "file"))
	assert.NilError(t, err)
	assert.Equal(t, fileInfo.Mode().Perm(), os.FileMode(0o640))

	execInfo, err := os.Stat(filepath.Join(dst, "exec"))
	assert.NilError(t, err)
	assert.Equal(t, execInfo.Mode().Perm(), os.FileMode(0o755))

	dirInfo, err := os.Stat(filepath.Join(dst, "inner"))
	assert.NilError(t, err)
	assert.Equal(t, dirInfo.Mode().Perm(), os.FileMode(0o555))

	linkInfo, err := os.Lstat(filepath.Join(dst, "link"))
	assert.NilError(t, err)
	assert.Assert(t, linkInfo.Mode()&os.ModeSymlink != 0)

	target, err := os.Readlink(filepath.Join(dst, "link"))
	assert.NilError(t, err)
	assert.Equal(t, target, "inner/file")

	// Copying onto an existing destination is an error, never an overwrite.
	err = c.CopyTree(filepath.Join(src, "dir"), dst, info)
	assert.Assert(t, errors.Is(err, os.ErrExist), err)
}

func TestCopierSkipsSpecialFiles(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	fifo := filepath.Join(src, "fifo")
	assert.NilError(t, syscall.Mkfifo(fifo, 0o644))

	info, err := os.Lstat(fifo)
	assert.NilError(t, err)

	var warnings []string

	c := newCopier(CopyPlain, func(msg string) { warnings = append(warnings, msg) })
	assert.NilError(t, c.CopyEntry(fifo, filepath.Join(t.TempDir(), "fifo"), info))
	assert.Equal(t, int(c.skipped.Load()), 1)
	assert.Equal(t, len(warnings), 1)
}
