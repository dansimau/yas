package workyard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"gotest.tools/v3/assert"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	t.Run("missing file is zero config", func(t *testing.T) {
		t.Parallel()

		cfg, err := LoadConfig(t.TempDir())
		assert.NilError(t, err)
		assert.DeepEqual(t, cfg, Config{})
	})

	t.Run("parses trunk and per-repo overrides", func(t *testing.T) {
		t.Parallel()

		source := t.TempDir()
		writeConfig(t, source, "version: 1\ntrunk: main\nrepos:\n  wt_core: { trunk: develop }\n")

		cfg, err := LoadConfig(source)
		assert.NilError(t, err)
		assert.Equal(t, cfg.Trunk, "main")
		assert.Equal(t, cfg.trunkFor("wt_core"), "develop")
		assert.Equal(t, cfg.trunkFor("other"), "main")
	})

	t.Run("unknown keys are rejected", func(t *testing.T) {
		t.Parallel()

		source := t.TempDir()
		writeConfig(t, source, "trunk: main\nignore: ['*.log']\n")

		_, err := LoadConfig(source)
		assert.ErrorContains(t, err, "ignore")
	})

	t.Run("future versions are rejected", func(t *testing.T) {
		t.Parallel()

		source := t.TempDir()
		writeConfig(t, source, "version: 99\n")

		_, err := LoadConfig(source)
		assert.ErrorContains(t, err, "unsupported version")
	})
}

func writeConfig(t *testing.T, source string, content string) {
	t.Helper()

	assert.NilError(t, os.MkdirAll(filepath.Join(source, workyardDir), 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(source, workyardDir, configFile), []byte(content), 0o644))
}

func TestFind(t *testing.T) {
	source := t.TempDir()
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b", "c")
	assert.NilError(t, os.MkdirAll(nested, 0o755))

	id := yardID(root)
	assert.NilError(t, writeMetadata(Metadata{Version: MetadataVersion, ID: id, Source: source, Target: root, Branch: "feature", Complete: true}))
	assert.NilError(t, writePointer(root, source, id))

	yard, err := Find(nested)
	assert.NilError(t, err)
	assert.Equal(t, yard.Root, root)
	assert.Equal(t, yard.Source, source)
	assert.Equal(t, yard.ID, id)
	assert.Equal(t, yard.Meta.Branch, "feature")

	// The source's own .workyard directory does not make it a yard.
	_, err = Find(filepath.Join(source, workyardDir))
	assert.ErrorIs(t, err, ErrNotAWorkyard)

	_, err = Find(t.TempDir())
	assert.ErrorIs(t, err, ErrNotAWorkyard)

	// WORKYARD_ROOT wins over the working directory.
	t.Setenv(RootEnvVar, root)

	yard, err = Find(t.TempDir())
	assert.NilError(t, err)
	assert.Equal(t, yard.Meta.Branch, "feature")

	t.Setenv(RootEnvVar, t.TempDir())

	_, err = Find(nested)
	assert.ErrorIs(t, err, ErrNotAWorkyard)

	// Removing the metadata leaves the yards directory empty, so it goes too.
	assert.NilError(t, removeMetadata(source, id))
	_, err = os.Stat(filepath.Join(source, workyardDir))
	assert.Assert(t, os.IsNotExist(err))

	// A yard whose source is gone is reported as such.
	assert.NilError(t, os.RemoveAll(source))

	_, err = Open(root)

	missing := &SourceMissingError{}
	assert.Assert(t, errors.As(err, &missing), err)
	assert.Equal(t, missing.Source, source)
}

func TestIsWithinAndRealPath(t *testing.T) {
	t.Parallel()

	assert.Assert(t, isWithin("/a/b", "/a/b"))
	assert.Assert(t, isWithin("/a/b", "/a/b/c"))
	assert.Assert(t, !isWithin("/a/b", "/a/bc"))
	assert.Assert(t, !isWithin("/a/b", "/a"))
	assert.Assert(t, !isWithin("/a/b/c", "/a/b"))

	dir := t.TempDir()
	realDir, err := filepath.EvalSymlinks(dir)
	assert.NilError(t, err)

	link := filepath.Join(dir, "link")
	assert.NilError(t, os.Symlink(realDir, link))

	// A path that does not exist yet resolves through its existing ancestors.
	got, err := realPath(filepath.Join(link, "new", "deeper"))
	assert.NilError(t, err)
	assert.Equal(t, got, filepath.Join(realDir, "new", "deeper"))
}

func TestScan(t *testing.T) {
	t.Parallel()

	source := t.TempDir()

	assert.NilError(t, os.MkdirAll(filepath.Join(source, "plain", "nested"), 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(source, "plain", "nested", "n.txt"), []byte("n"), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(source, "top.txt"), []byte("t"), 0o644))
	assert.NilError(t, os.Symlink("top.txt", filepath.Join(source, "link")))
	assert.NilError(t, os.MkdirAll(filepath.Join(source, "sub", "deep"), 0o755))
	assert.NilError(t, os.MkdirAll(filepath.Join(source, workyardDir), 0o755))

	for _, repo := range []string{"repoA", "sub/deep/repoB"} {
		path := filepath.Join(source, repo)
		assert.NilError(t, os.MkdirAll(path, 0o755))
		testutil.ExecOrFail(t, path, "git init -q")
	}

	// A bare repository and a linked worktree (.git file) both count.
	bare := filepath.Join(source, "bare.git")
	assert.NilError(t, os.MkdirAll(bare, 0o755))
	testutil.ExecOrFail(t, bare, "git init -q --bare")
	testutil.ExecOrFail(t, filepath.Join(source, "repoA"), `
		git commit -q --allow-empty -m init
		git worktree add -q ../linked
	`)

	// A symlink to a repository is a leaf, not a repository.
	assert.NilError(t, os.Symlink("repoA", filepath.Join(source, "repolink")))

	plan, err := Scan(context.Background(), source)
	assert.NilError(t, err)

	var paths []string
	for _, repo := range plan.Repos {
		paths = append(paths, repo.Repo.Path)
	}

	assert.DeepEqual(t, paths, []string{"bare.git", "linked", "repoA", "sub/deep/repoB"})
	assert.Equal(t, plan.Subtrees, 1, "plain/ is one cloneable subtree")
	assert.Equal(t, plan.Files, 3, "top.txt, link and repolink are copied individually")
	assert.Assert(t, plan.Root != nil)

	var kinds []entryKind
	for _, child := range plan.Root.Children {
		kinds = append(kinds, child.Kind)
	}

	assert.Assert(t, !containsKind(plan.Root.Children, workyardDir), "the source .workyard dir is not copied")
	assert.Equal(t, len(kinds), 8, "plain, top.txt, link, sub, repoA, bare.git, linked, repolink")
}

func containsKind(children []*entry, name string) bool {
	for _, child := range children {
		if filepath.Base(child.Rel) == name {
			return true
		}
	}

	return false
}
