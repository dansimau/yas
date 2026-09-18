package tests

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestCreate_Tree(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target, result := f.create(t)
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())

	// The only output is the final status line.
	assert.Equal(t, result.Stdout(), "")
	assert.Assert(t, strings.HasPrefix(output(result.Stderr()), "Created workyard in "), result.Stderr())
	assert.Equal(t, strings.Count(output(result.Stderr()), "\n"), 1, result.Stderr())

	// Plain files, nested directories and symlinks are copied as-is.
	assertFileContent(t, filepath.Join(target, "top.txt"), "hello\n")
	assertFileContent(t, filepath.Join(target, "plain", "nested", "n.txt"), "nested\n")
	assertFileContent(t, filepath.Join(target, "readonly", "inside.txt"), "ro\n")

	linkInfo, err := os.Lstat(filepath.Join(target, "link"))
	assert.NilError(t, err)
	assert.Assert(t, linkInfo.Mode()&fs.ModeSymlink != 0, "link must stay a symlink")

	linkTarget, err := os.Readlink(filepath.Join(target, "link"))
	assert.NilError(t, err)
	assert.Equal(t, linkTarget, "top.txt")

	// Modes are preserved.
	readonlyInfo, err := os.Stat(filepath.Join(target, "readonly"))
	assert.NilError(t, err)
	assert.Equal(t, readonlyInfo.Mode().Perm(), fs.FileMode(0o555))

	execInfo, err := os.Stat(filepath.Join(target, "exec.sh"))
	assert.NilError(t, err)
	assert.Equal(t, execInfo.Mode().Perm(), fs.FileMode(0o755))

	// Repositories are worktrees: .git is a file, never a directory.
	assert.NilError(t, filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.Name() == ".git" {
			assert.Assert(t, !d.IsDir(), "%s must be a worktree gitfile", path)
		}

		return nil
	}))

	for _, repo := range []string{"repoA", "sub/deep/repoB", "wt/main"} {
		assert.Equal(t, currentBranch(t, filepath.Join(target, repo)), "feature", repo)
	}

	// The second worktree sharing a git directory is detached at trunk.
	assert.Equal(t, currentBranch(t, filepath.Join(target, "wt", "other")), "")
	assert.Equal(t,
		headHash(t, filepath.Join(target, "wt", "other"), "HEAD"),
		headHash(t, filepath.Join(f.Source, "wt", "main"), "main"))

	// Writing in the yard does not touch the source.
	assert.NilError(t, os.WriteFile(filepath.Join(target, "plain", "nested", "n.txt"), []byte("changed\n"), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(target, "repoA", "new.txt"), []byte("new\n"), 0o644))
	assertFileContent(t, filepath.Join(f.Source, "plain", "nested", "n.txt"), "nested\n")
	assertNotExists(t, filepath.Join(f.Source, "repoA", "new.txt"))

	// The yard holds only a pointer file; the metadata lives in the source.
	pointer, err := os.Lstat(filepath.Join(target, ".workyard"))
	assert.NilError(t, err)
	assert.Assert(t, pointer.Mode().IsRegular(), ".workyard in the yard must be a file")
	assert.Equal(t, len(yardMetadataFiles(t, f.Source)), 1)

	yard := openYard(t, target)
	assert.Assert(t, yard.Meta.Complete)
	assert.Equal(t, yard.Meta.Branch, "feature")
	assert.Assert(t, strings.HasSuffix(yard.Source, "/src"))

	var paths []string
	for _, repo := range yard.Meta.Repos {
		paths = append(paths, repo.Path)
	}

	assert.DeepEqual(t, paths, fixtureRepos)
	assert.Assert(t, yard.Meta.Repos[0].CreatedBranch)
	assert.Assert(t, !yard.Meta.Repos[0].Detached)
	assert.Assert(t, yard.Meta.Repos[3].Detached)

	// Each source repository knows about its new worktree.
	for _, repo := range []string{"repoA", "sub/deep/repoB"} {
		list := mustOutput(t, filepath.Join(f.Source, repo), "git", "worktree", "list")
		assert.Assert(t, cmp.Contains(list, filepath.Join(target, repo)))
	}

	assert.Equal(t, worktreeCount(t, filepath.Join(f.Source, "wt", "main")), 4, "main, other and their two yard copies")
}

func TestCreate_DefaultBranchAndSourceAreCwdAndTargetName(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := filepath.Join(t.TempDir(), "my-feature")
	allowCleanup(t, filepath.Join(target, "readonly"))

	result := newCLI(t, f.Source).Run("create", "--target", target)
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, currentBranch(t, filepath.Join(target, "repoA")), "my-feature")
}

func TestCreate_SourceConfigIsNotCopied(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	assert.NilError(t, os.MkdirAll(filepath.Join(f.Source, ".workyard"), 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(f.Source, ".workyard", "config.yaml"), []byte("trunk: main\n"), 0o644))

	target := createYard(t, f)

	// The yard's .workyard is the pointer file, nothing else came along.
	info, err := os.Lstat(filepath.Join(target, ".workyard"))
	assert.NilError(t, err)
	assert.Assert(t, info.Mode().IsRegular())

	// Config and metadata sit side by side in the source.
	assertFileContent(t, filepath.Join(f.Source, ".workyard", "config.yaml"), "trunk: main\n")
	assert.Equal(t, len(yardMetadataFiles(t, f.Source)), 1)
}

func TestCreate_BranchResolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		setup  string // extra git commands run in the repository
		config string // contents of .workyard/config.yaml, if any
		branch string

		wantExit     int
		wantErr      string
		wantBranch   string // "" means detached
		wantHeadRef  string // ref in the source the yard's HEAD must equal
		wantCreated  bool
		wantDetached bool
		wantUpstream string
	}{
		{
			name:        "existing local branch",
			setup:       "git branch existing\ngit commit -q --allow-empty -m more",
			branch:      "existing",
			wantBranch:  "existing",
			wantHeadRef: "existing",
		},
		{
			name:     "checked out branch fails",
			branch:   "main",
			wantExit: 1,
			wantErr:  "already checked out",
		},
		{
			name: "remote-only branch is tracked",
			setup: `
				git init -q --bare REMOTE
				git remote add origin REMOTE
				git commit -q --allow-empty -m remote
				git branch remote-only
				git push -q origin main remote-only
				git branch -D remote-only
				git reset -q --hard HEAD~1
			`,
			branch:       "remote-only",
			wantBranch:   "remote-only",
			wantHeadRef:  "origin/remote-only",
			wantCreated:  true,
			wantUpstream: "origin/remote-only",
		},
		{
			name: "branch on two remotes is ambiguous",
			setup: `
				git update-ref refs/remotes/origin/dup HEAD
				git update-ref refs/remotes/upstream/dup HEAD
				git remote add origin https://example.com/a.git
				git remote add upstream https://example.com/b.git
			`,
			branch:   "dup",
			wantExit: 1,
			wantErr:  "more than one remote",
		},
		{
			name:         "tag is checked out detached",
			setup:        "git tag v1\ngit commit -q --allow-empty -m after-tag",
			branch:       "v1",
			wantHeadRef:  "v1",
			wantDetached: true,
		},
		{
			name:        "missing branch is created from main",
			branch:      "feature",
			wantBranch:  "feature",
			wantHeadRef: "main",
			wantCreated: true,
		},
		{
			name:        "missing branch is created from master",
			setup:       "git branch -m main master",
			branch:      "feature",
			wantBranch:  "feature",
			wantHeadRef: "master",
			wantCreated: true,
		},
		{
			name:        "missing branch is created from configured trunk",
			setup:       "git branch develop\ngit checkout -q develop\ngit commit -q --allow-empty -m dev\ngit checkout -q main",
			config:      "trunk: develop\n",
			branch:      "feature",
			wantBranch:  "feature",
			wantHeadRef: "develop",
			wantCreated: true,
		},
		{
			name:        "per-repo trunk overrides the global one",
			setup:       "git branch develop\ngit checkout -q develop\ngit commit -q --allow-empty -m dev\ngit checkout -q main",
			config:      "trunk: main\nrepos:\n  repo: { trunk: develop }\n",
			branch:      "feature",
			wantBranch:  "feature",
			wantHeadRef: "develop",
			wantCreated: true,
		},
		{
			name:     "no trunk to create from fails with a hint",
			setup:    "git branch -m main trunk-only",
			branch:   "feature",
			wantExit: 1,
			wantErr:  "set trunk in .workyard/config.yaml",
		},
		{
			name:     "configured trunk that does not exist fails",
			config:   "trunk: nope\n",
			branch:   "feature",
			wantExit: 1,
			wantErr:  `trunk "nope" does not exist`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			source := filepath.Join(t.TempDir(), "src")
			repo := filepath.Join(source, "repo")
			remote := filepath.Join(t.TempDir(), "remote.git")
			initRepo(t, repo, strings.ReplaceAll(tt.setup, "REMOTE", remote))

			if tt.config != "" {
				assert.NilError(t, os.MkdirAll(filepath.Join(source, ".workyard"), 0o755))
				assert.NilError(t, os.WriteFile(filepath.Join(source, ".workyard", "config.yaml"), []byte(tt.config), 0o644))
			}

			target := filepath.Join(t.TempDir(), "yard")
			result := newCLI(t, source).Run("create", "--source", source, "--branch", tt.branch, target)

			assert.Equal(t, result.ExitCode(), tt.wantExit, result.Stderr())

			if tt.wantExit != 0 {
				assert.Assert(t, cmp.Contains(result.Stderr(), tt.wantErr))
				assertNotExists(t, target)
				assert.Equal(t, worktreeCount(t, repo), 1, "no worktree may be left behind")
				assert.Equal(t, len(yardMetadataFiles(t, source)), 0, "no metadata may be left behind")

				return
			}

			yardRepo := filepath.Join(target, "repo")
			assert.Equal(t, currentBranch(t, yardRepo), tt.wantBranch)
			assert.Equal(t, headHash(t, yardRepo, "HEAD"), headHash(t, repo, tt.wantHeadRef))

			yard := openYard(t, target)
			assert.Equal(t, len(yard.Meta.Repos), 1)
			assert.Equal(t, yard.Meta.Repos[0].CreatedBranch, tt.wantCreated)
			assert.Equal(t, yard.Meta.Repos[0].Detached, tt.wantDetached)

			if tt.wantUpstream != "" {
				assert.Equal(t, mustOutput(t, yardRepo, "git", "rev-parse", "--abbrev-ref", "@{upstream}"), tt.wantUpstream)
			}

			// The source's checkout is untouched.
			assert.Equal(t, currentBranch(t, repo), strings.TrimSpace(mustOutput(t, repo, "git", "branch", "--show-current")))
		})
	}
}

func TestCreate_Guards(t *testing.T) {
	t.Parallel()

	t.Run("non-empty target", func(t *testing.T) {
		t.Parallel()

		f := setupFixture(t)
		target := filepath.Join(t.TempDir(), "yard")
		assert.NilError(t, os.MkdirAll(target, 0o755))
		assert.NilError(t, os.WriteFile(filepath.Join(target, "existing"), []byte("x"), 0o644))

		result := newCLI(t, f.Source).Run("create", "--source", f.Source, "--branch", "feature", target)
		assert.Equal(t, result.ExitCode(), 1)
		assert.Assert(t, cmp.Contains(result.Stderr(), "not empty"))
		assertNotExists(t, filepath.Join(target, ".workyard"))
	})

	t.Run("empty existing target is fine", func(t *testing.T) {
		t.Parallel()

		f := setupFixture(t)
		target := filepath.Join(t.TempDir(), "yard")
		assert.NilError(t, os.MkdirAll(target, 0o755))
		allowCleanup(t, filepath.Join(target, "readonly"))

		result := newCLI(t, f.Source).Run("create", "--source", f.Source, "--branch", "feature", target)
		assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	})

	t.Run("target inside source", func(t *testing.T) {
		t.Parallel()

		f := setupFixture(t)
		result := newCLI(t, f.Source).Run("create", "--source", f.Source, "--branch", "feature", filepath.Join(f.Source, "yard"))
		assert.Equal(t, result.ExitCode(), 1)
		assert.Assert(t, cmp.Contains(result.Stderr(), "must not contain each other"))
		assertNotExists(t, filepath.Join(f.Source, "yard"))
	})

	t.Run("source inside target", func(t *testing.T) {
		t.Parallel()

		f := setupFixture(t)
		result := newCLI(t, f.Source).Run("create", "--source", f.Source, "--branch", "feature", filepath.Dir(f.Source))
		assert.Equal(t, result.ExitCode(), 1)
		assert.Assert(t, cmp.Contains(result.Stderr(), "must not contain each other"))
	})

	t.Run("target inside the source's .workyard directory", func(t *testing.T) {
		t.Parallel()

		// The conventional place for yards, mirroring yas' .yas/worktrees.
		f := setupFixture(t)
		target := filepath.Join(f.Source, ".workyard", "yards", "feature")
		allowCleanup(t, filepath.Join(target, "readonly"))

		result := newCLI(t, f.Source).Run("create", "--source", f.Source, target)
		assert.Equal(t, result.ExitCode(), 0, result.Stderr())
		assertExists(t, filepath.Join(target, "repoA"))
		// The source's .workyard directory is not copied into the yard, whose
		// own .workyard is just the pointer file.
		pointer, err := os.Lstat(filepath.Join(target, ".workyard"))
		assert.NilError(t, err)
		assert.Assert(t, pointer.Mode().IsRegular())
		assert.Equal(t, len(yardMetadataFiles(t, f.Source)), 1)
		assert.Equal(t, mustOutput(t, filepath.Join(target, "repoA"), "git", "branch", "--show-current"), "feature")

		// A second yard next to it must not disturb the first.
		second := filepath.Join(f.Source, ".workyard", "yards", "feature2")
		allowCleanup(t, filepath.Join(second, "readonly"))

		result = newCLI(t, f.Source).Run("create", "--source", f.Source, second)
		assert.Equal(t, result.ExitCode(), 0, result.Stderr())
		assert.Equal(t, len(yardMetadataFiles(t, f.Source)), 2)

		// Commands find the yard from inside it, and remove cleans up only it.
		result = newCLI(t, filepath.Join(target, "plain")).Run("ls")
		assert.Equal(t, result.ExitCode(), 0, result.Stderr())
		assert.Assert(t, cmp.Contains(result.Stdout(), "repoA"))

		result = newCLI(t, f.Source).Run("remove", target)
		assert.Equal(t, result.ExitCode(), 0, result.Stderr())
		assertNotExists(t, target)
		assertExists(t, filepath.Join(second, "repoA"))
		assert.Equal(t, len(yardMetadataFiles(t, f.Source)), 1)
	})

	t.Run("target that would swallow the source's metadata", func(t *testing.T) {
		t.Parallel()

		f := setupFixture(t)

		for _, target := range []string{
			filepath.Join(f.Source, ".workyard"),
			filepath.Join(f.Source, ".workyard", "yards"),
		} {
			result := newCLI(t, f.Source).Run("create", "--source", f.Source, "--branch", "feature", target)
			assert.Equal(t, result.ExitCode(), 1, target)
			assert.Assert(t, cmp.Contains(result.Stderr(), "must not contain each other"))
			assertNotExists(t, target)
		}
	})

	t.Run("source that is a repository", func(t *testing.T) {
		t.Parallel()

		f := setupFixture(t)
		target := filepath.Join(t.TempDir(), "yard")

		result := newCLI(t, f.Source).Run("create", "--source", filepath.Join(f.Source, "repoA"), "--branch", "feature", target)
		assert.Equal(t, result.ExitCode(), 1)
		assert.Assert(t, cmp.Contains(result.Stderr(), "Workyard cannot be a git repository. Create a git worktree instead"))
		assertNotExists(t, target)
		assert.Equal(t, worktreeCount(t, filepath.Join(f.Source, "repoA")), 1)
	})

	t.Run("source inside a repository working tree", func(t *testing.T) {
		t.Parallel()

		f := setupFixture(t)
		inside := filepath.Join(f.Source, "repoA", "dir")
		assert.NilError(t, os.MkdirAll(inside, 0o755))

		result := newCLI(t, f.Source).Run("create", "--source", inside, "--branch", "feature", filepath.Join(t.TempDir(), "yard"))
		assert.Equal(t, result.ExitCode(), 1)
		assert.Assert(t, cmp.Contains(result.Stderr(), "Workyard cannot be a git repository"))
	})

	t.Run("source that is a bare repository", func(t *testing.T) {
		t.Parallel()

		bare := filepath.Join(t.TempDir(), "bare.git")
		assert.NilError(t, os.MkdirAll(bare, 0o755))
		mustOutput(t, bare, "git", "init", "-q", "--bare")

		result := newCLI(t, bare).Run("create", "--source", bare, "--branch", "feature", filepath.Join(t.TempDir(), "yard"))
		assert.Equal(t, result.ExitCode(), 1)
		assert.Assert(t, cmp.Contains(result.Stderr(), "Workyard cannot be a git repository"))
	})

	t.Run("invalid branch name", func(t *testing.T) {
		t.Parallel()

		f := setupFixture(t)
		result := newCLI(t, f.Source).Run("create", "--source", f.Source, "--branch", "bad..name", filepath.Join(t.TempDir(), "yard"))
		assert.Equal(t, result.ExitCode(), 1)
		assert.Assert(t, cmp.Contains(result.Stderr(), "invalid branch name"))
	})

	t.Run("source that is a workyard", func(t *testing.T) {
		t.Parallel()

		f := setupFixture(t)
		first := createYard(t, f)

		for _, source := range []string{first, filepath.Join(first, "plain")} {
			second := filepath.Join(t.TempDir(), "second")
			result := newCLI(t, first).Run("create", "--source", source, "--branch", "feature2", second)
			assert.Equal(t, result.ExitCode(), 1)
			assert.Assert(t, cmp.Contains(result.Stderr(), "inside a workyard"))
			assertNotExists(t, second)
		}
	})

	t.Run("dry run creates nothing", func(t *testing.T) {
		t.Parallel()

		f := setupFixture(t)
		target := filepath.Join(t.TempDir(), "yard")

		result := newCLI(t, f.Source).Run("create", "--dry-run", "--source", f.Source, "--branch", "feature", target)
		assert.Equal(t, result.ExitCode(), 0, result.Stderr())
		assert.Assert(t, cmp.Contains(result.Stdout(), "repositories: 4"))
		assert.Assert(t, cmp.Contains(result.Stdout(), "repoA: create branch feature from main"))
		assert.Assert(t, cmp.Contains(result.Stdout(), "wt/other: detached at main"))
		assert.Assert(t, cmp.Contains(result.Stdout(), "plain/ (subtree)"))
		assertNotExists(t, target)
		assert.Equal(t, len(yardMetadataFiles(t, f.Source)), 0)

		for _, repo := range []string{"repoA", "sub/deep/repoB"} {
			assert.Equal(t, worktreeCount(t, filepath.Join(f.Source, repo)), 1)
		}
	})

	t.Run("target given twice or not at all", func(t *testing.T) {
		t.Parallel()

		f := setupFixture(t)
		cli := newCLI(t, f.Source)

		result := cli.Run("create", "--source", f.Source)
		assert.Equal(t, result.ExitCode(), 2)
		assert.Assert(t, cmp.Contains(result.Stderr(), "no target"))

		result = cli.Run("create", "--source", f.Source, "--target", "a", "b")
		assert.Equal(t, result.ExitCode(), 2)
	})
}

func TestCreate_RollsBackOnFailure(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)

	// Making .git/worktrees a file makes "git worktree add" fail for that
	// repository only, after planning has succeeded.
	assert.NilError(t, os.WriteFile(filepath.Join(f.Source, "sub", "deep", "repoB", ".git", "worktrees"), []byte("x"), 0o644))

	target, result := f.create(t)
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, cmp.Contains(result.Stderr(), "sub/deep/repoB"))
	assert.Assert(t, !strings.Contains(result.Stderr(), "Created workyard"))
	assertNotExists(t, target)

	// No worktrees, created branches or metadata are left behind.
	for _, repo := range []string{"repoA", "wt/main"} {
		dir := filepath.Join(f.Source, repo)
		assert.Assert(t, !strings.Contains(mustOutput(t, dir, "git", "worktree", "list"), target), "stale worktree left in %s", repo)
		assert.Equal(t, mustOutput(t, dir, "git", "branch", "--list", "feature"), "", "created branch left in %s", repo)
	}

	assert.Equal(t, mustOutput(t, filepath.Join(f.Source, "repoA"), "git", "branch", "--list", "existing"), "existing", "pre-existing branches are kept")
	assert.Equal(t, len(yardMetadataFiles(t, f.Source)), 0)
	assertNotExists(t, filepath.Join(f.Source, ".workyard"))
}

func TestCreate_VerboseListsIgnoredFilesAndSubmodules(t *testing.T) {
	t.Parallel()

	source := filepath.Join(t.TempDir(), "src")
	sub := filepath.Join(t.TempDir(), "sub")
	initRepo(t, sub, "")
	initRepo(t, filepath.Join(source, "repo"), `
		echo 'node_modules/' > .gitignore
		git add .gitignore
		git -c protocol.file.allow=always submodule add -q `+sub+` vendored
		git commit -q -m "gitignore and submodule"
		mkdir -p node_modules/pkg
		touch node_modules/pkg/index.js
	`)

	target := filepath.Join(t.TempDir(), "yard")

	// Without -v the notes are not shown.
	result := newCLI(t, source).Run("create", "--source", source, "--branch", "feature", target)
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Assert(t, !strings.Contains(output(result.Stderr()), "submodule"), result.Stderr())

	verboseTarget := filepath.Join(t.TempDir(), "yard-v")
	result = newCLI(t, source).Run("-v", "create", "--source", source, "--branch", "feature-v", verboseTarget)
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Assert(t, cmp.Contains(result.Stderr(), "repo: ignored in source, not copied: node_modules/"))
	assert.Assert(t, cmp.Contains(result.Stderr(), "repo has 1 submodule(s)"))
	assertNotExists(t, filepath.Join(verboseTarget, "repo", "node_modules"))

	yard := openYard(t, verboseTarget)
	assert.Equal(t, yard.Meta.Repos[0].Submodules, 1)
}
