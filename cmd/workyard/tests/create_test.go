package tests

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestCreate_Tree(t *testing.T) {
	t.Parallel()

	for _, mode := range copyModes {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			f := setupFixture(t)
			target, result := f.create(t, mode)
			assert.Equal(t, result.ExitCode(), 0, result.Stderr())

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

			// Metadata records every repository and that creation completed.
			yard := openYard(t, target)
			assert.Assert(t, yard.Meta.Complete)
			assert.Equal(t, yard.Meta.Branch, "feature")

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

			// The footer reports what happened.
			assert.Assert(t, cmp.Contains(result.Stderr(), "4 repositories"))
			assert.Assert(t, cmp.Contains(result.Stderr(), "ignored by git are not copied"))

			if mode == "plain" || runtime.GOOS != "darwin" {
				assert.Assert(t, cmp.Contains(result.Stderr(), "0 cloned"))
			} else {
				assert.Assert(t, cmp.Contains(result.Stderr(), "0 copied"))
			}
		})
	}
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

func TestCreate_BranchResolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		setup  string // extra git commands run in the repository
		config string // contents of .workyard/config.yaml, if any
		branch string
		args   []string

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
			name:     "checked out branch fails without --detach",
			branch:   "main",
			wantExit: 1,
			wantErr:  "already checked out",
		},
		{
			name:         "checked out branch detaches with --detach",
			branch:       "main",
			args:         []string{"--detach"},
			wantHeadRef:  "main",
			wantDetached: true,
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
			args := append([]string{"create", "--source", source, "--branch", tt.branch, target}, tt.args...)
			result := newCLI(t, source).Run(args...)

			assert.Equal(t, result.ExitCode(), tt.wantExit, result.Stderr())

			if tt.wantExit != 0 {
				assert.Assert(t, cmp.Contains(result.Stderr(), tt.wantErr))
				assertNotExists(t, target)
				assert.Equal(t, worktreeCount(t, repo), 1, "no worktree may be left behind")

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

	t.Run("source inside a repository working tree", func(t *testing.T) {
		t.Parallel()

		f := setupFixture(t)
		inside := filepath.Join(f.Source, "repoA", "dir")
		assert.NilError(t, os.MkdirAll(inside, 0o755))

		result := newCLI(t, f.Source).Run("create", "--source", inside, "--branch", "feature", filepath.Join(t.TempDir(), "yard"))
		assert.Equal(t, result.ExitCode(), 1)
		assert.Assert(t, cmp.Contains(result.Stderr(), "inside the git repository"))
	})

	t.Run("source that is a repository root becomes a single worktree", func(t *testing.T) {
		t.Parallel()

		f := setupFixture(t)
		target := filepath.Join(t.TempDir(), "yard")

		result := newCLI(t, f.Source).Run("create", "--source", filepath.Join(f.Source, "repoA"), "--branch", "feature", target)
		assert.Equal(t, result.ExitCode(), 0, result.Stderr())
		assert.Equal(t, currentBranch(t, target), "feature")

		yard := openYard(t, target)
		assert.Equal(t, yard.Meta.Repos[0].Path, ".")

		// And can be removed again, metadata and all.
		result = newCLI(t, t.TempDir()).Run("remove", target)
		assert.Equal(t, result.ExitCode(), 0, result.Stderr())
		assertNotExists(t, target)
		assert.Equal(t, worktreeCount(t, filepath.Join(f.Source, "repoA")), 1)
	})

	t.Run("invalid branch name", func(t *testing.T) {
		t.Parallel()

		f := setupFixture(t)
		result := newCLI(t, f.Source).Run("create", "--source", f.Source, "--branch", "bad..name", filepath.Join(t.TempDir(), "yard"))
		assert.Equal(t, result.ExitCode(), 1)
		assert.Assert(t, cmp.Contains(result.Stderr(), "invalid branch name"))
	})

	t.Run("nested workyard needs --allow-nested", func(t *testing.T) {
		t.Parallel()

		f := setupFixture(t)
		first := createYard(t, f)

		second := filepath.Join(t.TempDir(), "second")
		result := newCLI(t, first).Run("create", "--source", first, "--branch", "feature2", second)
		assert.Equal(t, result.ExitCode(), 1)
		assert.Assert(t, cmp.Contains(result.Stderr(), "--allow-nested"))
		assertNotExists(t, second)

		allowCleanup(t, filepath.Join(second, "readonly"))
		result = newCLI(t, first).Run("create", "--allow-nested", "--source", first, "--branch", "feature2", second)
		assert.Equal(t, result.ExitCode(), 0, result.Stderr())
		assert.Equal(t, currentBranch(t, filepath.Join(second, "repoA")), "feature2")

		// The nested yard's worktrees belong to the original source repos.
		assert.Equal(t, worktreeCount(t, filepath.Join(f.Source, "repoA")), 3)
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

func TestCreate_Rollback(t *testing.T) {
	t.Parallel()

	// Making .git/worktrees a file makes "git worktree add" fail for that
	// repository only, after planning has succeeded.
	breakRepo := func(t *testing.T, f fixture) {
		t.Helper()

		assert.NilError(t, os.WriteFile(filepath.Join(f.Source, "sub", "deep", "repoB", ".git", "worktrees"), []byte("x"), 0o644))
	}

	t.Run("rolls back by default", func(t *testing.T) {
		t.Parallel()

		f := setupFixture(t)
		breakRepo(t, f)

		target, result := f.create(t, "auto")
		assert.Equal(t, result.ExitCode(), 1)
		assert.Assert(t, cmp.Contains(result.Stderr(), "sub/deep/repoB"))
		assert.Assert(t, cmp.Contains(result.Stderr(), "rolling back"))
		assertNotExists(t, target)

		for _, repo := range []string{"repoA", "wt/main"} {
			list := mustOutput(t, filepath.Join(f.Source, repo), "git", "worktree", "list")
			assert.Assert(t, !strings.Contains(list, target), "stale worktree left in %s: %s", repo, list)
		}
	})

	t.Run("keeps a partial yard with --keep-partial", func(t *testing.T) {
		t.Parallel()

		f := setupFixture(t)
		breakRepo(t, f)

		target, result := f.create(t, "auto", "--keep-partial")
		assert.Equal(t, result.ExitCode(), 1)
		assert.Assert(t, cmp.Contains(result.Stderr(), "keeping partial workyard"))
		assertExists(t, target)

		yard := openYard(t, target)
		assert.Assert(t, !yard.Meta.Complete)
		assert.Equal(t, currentBranch(t, filepath.Join(target, "repoA")), "feature")
		assertNotExists(t, filepath.Join(target, "sub", "deep", "repoB", ".git"))

		// An incomplete yard is still usable and removable.
		result = newCLI(t, target).Run("ls")
		assert.Equal(t, result.ExitCode(), 0, result.Stderr())
		assert.Assert(t, cmp.Contains(result.Stdout(), "not completely created"))

		result = newCLI(t, t.TempDir()).Run("remove", target)
		assert.Equal(t, result.ExitCode(), 0, result.Stderr())
		assertNotExists(t, target)
	})
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
	testutil.ExecOrFail(t, source, "true")

	target := filepath.Join(t.TempDir(), "yard")
	result := newCLI(t, source).Run("-v", "create", "--source", source, "--branch", "feature", target)
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Assert(t, cmp.Contains(result.Stderr(), "repo: ignored in source, not copied: node_modules/"))
	assert.Assert(t, cmp.Contains(result.Stderr(), "repo has 1 submodule(s)"))
	assertNotExists(t, filepath.Join(target, "repo", "node_modules"))

	yard := openYard(t, target)
	assert.Equal(t, yard.Meta.Repos[0].Submodules, 1)
}
