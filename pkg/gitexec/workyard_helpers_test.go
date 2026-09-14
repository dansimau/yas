package gitexec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"gotest.tools/v3/assert"
)

func TestCommonDirAndTopLevel(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	setupRepo(t, repoPath, "")

	repo := WithRepo(repoPath)

	commonDir, err := repo.CommonDir()
	assert.NilError(t, err)

	realRepoPath, err := filepath.EvalSymlinks(repoPath)
	assert.NilError(t, err)
	assert.Equal(t, commonDir, filepath.Join(realRepoPath, ".git"))

	top, err := repo.TopLevel()
	assert.NilError(t, err)
	assert.Equal(t, top, realRepoPath)

	// A linked worktree shares the common dir but has its own top level.
	worktreePath := filepath.Join(t.TempDir(), "wt")
	assert.NilError(t, repo.WorktreeAdd(worktreePath, "wt-branch", "HEAD"))

	worktreeCommonDir, err := WithRepo(worktreePath).CommonDir()
	assert.NilError(t, err)
	assert.Equal(t, worktreeCommonDir, commonDir)

	// Outside any repository there is no top level.
	_, err = WithRepo(t.TempDir()).TopLevel()
	assert.Assert(t, err != nil)
}

func TestIsCommitish(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	setupRepo(t, repoPath, `
		git tag v1
	`)

	repo := WithRepo(repoPath)

	for ref, expected := range map[string]bool{
		"HEAD":         true,
		"v1":           true,
		"main":         true,
		"nonexistent":  false,
		"refs/tags/v1": true,
	} {
		ok, err := repo.IsCommitish(ref)
		assert.NilError(t, err, ref)
		assert.Equal(t, ok, expected, ref)
	}
}

func TestRemoteBranchRefs(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	setupRepo(t, repoPath, `
		git update-ref refs/remotes/origin/feature HEAD
		git update-ref refs/remotes/upstream/feature HEAD
		git update-ref refs/remotes/origin/other HEAD
		git update-ref refs/remotes/notaremote/feature HEAD
		git remote add origin https://example.com/origin.git
		git remote add upstream https://example.com/upstream.git
	`)

	repo := WithRepo(repoPath)

	refs, err := repo.RemoteBranchRefs("feature")
	assert.NilError(t, err)
	assert.DeepEqual(t, refs, []string{"origin/feature", "upstream/feature"})

	refs, err = repo.RemoteBranchRefs("other")
	assert.NilError(t, err)
	assert.DeepEqual(t, refs, []string{"origin/other"})

	refs, err = repo.RemoteBranchRefs("missing")
	assert.NilError(t, err)
	assert.Equal(t, len(refs), 0)
}

func TestCheckBranchName(t *testing.T) {
	t.Parallel()

	repo := WithRepo(t.TempDir())

	assert.NilError(t, repo.CheckBranchName("feature/x-1"))
	assert.ErrorContains(t, repo.CheckBranchName("bad..name"), "invalid branch name")
	assert.ErrorContains(t, repo.CheckBranchName("-leading-dash"), "invalid branch name")
}

func TestWorktreeAddVariantsAndRemoveForce(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	setupRepo(t, repoPath, `
		git update-ref refs/remotes/origin/remote-only HEAD
		git remote add origin https://example.com/origin.git
		echo '#!/bin/sh' > hook.sh
		echo 'echo hook ran > "$(git rev-parse --show-toplevel)/../hook-ran"' >> hook.sh
		chmod +x hook.sh
		git config core.hooksPath hooks
		mkdir hooks
		cp hook.sh hooks/post-checkout
	`)

	repo := WithRepo(repoPath)
	worktrees := t.TempDir()

	detached := filepath.Join(worktrees, "detached")
	assert.NilError(t, repo.WorktreeAddDetached(detached, "HEAD"))

	_, err := WithRepo(detached).GetCurrentBranchName()
	assert.ErrorIs(t, err, ErrDetachedHead)

	tracking := filepath.Join(worktrees, "tracking")
	assert.NilError(t, repo.WorktreeAddTracking(tracking, "remote-only", "origin/remote-only"))

	branch, err := WithRepo(tracking).GetCurrentBranchName()
	assert.NilError(t, err)
	assert.Equal(t, branch, "remote-only")

	upstream, err := repo.GetConfig("branch.remote-only.merge")
	assert.NilError(t, err)
	assert.Equal(t, upstream, "refs/heads/remote-only")

	// Hooks are disabled while adding worktrees.
	_, err = os.Stat(filepath.Join(worktrees, "hook-ran"))
	assert.Assert(t, os.IsNotExist(err), "post-checkout hook should not have run")

	// A dirty worktree needs --force; a locked one needs it twice.
	assert.NilError(t, os.WriteFile(filepath.Join(detached, "dirty"), []byte("x"), 0o644))
	assert.Assert(t, repo.WorktreeRemoveForce(detached, 0) != nil)
	assert.NilError(t, repo.WorktreeRemoveForce(detached, 1))

	testutil.ExecOrFail(t, repoPath, "git worktree lock "+tracking)
	assert.NilError(t, os.WriteFile(filepath.Join(tracking, "dirty"), []byte("x"), 0o644))
	assert.Assert(t, repo.WorktreeRemoveForce(tracking, 1) != nil)
	assert.NilError(t, repo.WorktreeRemoveForce(tracking, 2))

	list, err := repo.Worktrees()
	assert.NilError(t, err)
	assert.Equal(t, len(list), 1)
}

func TestWorktreePrune(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	setupRepo(t, repoPath, "")

	repo := WithRepo(repoPath)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	assert.NilError(t, repo.WorktreeAddDetached(worktreePath, "HEAD"))
	assert.NilError(t, os.RemoveAll(worktreePath))

	list, err := repo.Worktrees()
	assert.NilError(t, err)
	assert.Equal(t, len(list), 2, "stale entry is still listed before pruning")

	assert.NilError(t, repo.WorktreePrune())

	list, err = repo.Worktrees()
	assert.NilError(t, err)
	assert.Equal(t, len(list), 1)
}

func TestSubmoduleCountAndIgnoredTopLevel(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	setupRepo(t, repoPath, `
		echo 'build/' > .gitignore
		echo '*.log' >> .gitignore
		git add .gitignore
		git commit -m gitignore
		mkdir -p build/inner
		touch build/inner/a build/b out.log kept.txt
	`)

	repo := WithRepo(repoPath)

	count, err := repo.SubmoduleCount()
	assert.NilError(t, err)
	assert.Equal(t, count, 0)

	ignored, err := repo.IgnoredTopLevel()
	assert.NilError(t, err)
	assert.DeepEqual(t, ignored, []string{"build/", "out.log"})

	// Add a submodule pointing at a sibling repository.
	subPath := t.TempDir()
	setupRepo(t, subPath, "")
	testutil.ExecOrFail(t, repoPath, `
		git -c protocol.file.allow=always submodule add -q `+subPath+` sub
		git commit -q -m submodule
	`)

	count, err = repo.SubmoduleCount()
	assert.NilError(t, err)
	assert.Equal(t, count, 1)
}

func TestDeleteBranchSafe(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	setupRepo(t, repoPath, `
		git branch merged
		git checkout -b unmerged
		git commit --allow-empty -m "unmerged commit"
		git checkout main
	`)

	repo := WithRepo(repoPath)

	assert.NilError(t, repo.DeleteBranchSafe("merged"))

	err := repo.DeleteBranchSafe("unmerged")
	assert.Assert(t, err != nil)

	exists, err := repo.BranchExists("unmerged")
	assert.NilError(t, err)
	assert.Assert(t, exists, "unmerged branch must survive a safe delete")
}
