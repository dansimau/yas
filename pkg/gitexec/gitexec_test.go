package gitexec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"gotest.tools/v3/assert"
)

func TestDetectMainBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setup          func(t *testing.T, repoPath string)
		expectedBranch string
	}{
		{
			name: "detects local main branch",
			setup: func(t *testing.T, repoPath string) {
				testutil.ExecOrFail(t, repoPath, `
					git init
					git checkout -b main
					git config user.email "test@example.com"
					git config user.name "Test User"
					git commit --allow-empty -m "initial commit"
				`)
			},
			expectedBranch: "main",
		},
		{
			name: "detects local master branch",
			setup: func(t *testing.T, repoPath string) {
				testutil.ExecOrFail(t, repoPath, `
					git init
					git checkout -b master
					git config user.email "test@example.com"
					git config user.name "Test User"
					git commit --allow-empty -m "initial commit"
				`)
			},
			expectedBranch: "master",
		},
		{
			name: "prefers main over master when both exist",
			setup: func(t *testing.T, repoPath string) {
				testutil.ExecOrFail(t, repoPath, `
					git init
					git checkout -b main
					git config user.email "test@example.com"
					git config user.name "Test User"
					git commit --allow-empty -m "initial commit"
					git checkout -b master
				`)
			},
			expectedBranch: "main",
		},
		{
			name: "detects remote main branch",
			setup: func(t *testing.T, repoPath string) {
				remoteDir := filepath.Join(t.TempDir(), "remote.git")
				testutil.ExecOrFail(t, repoPath, `
					git init --bare `+remoteDir+`
					git init
					git checkout -b main
					git config user.email "test@example.com"
					git config user.name "Test User"
					git commit --allow-empty -m "initial commit"
					git remote add origin `+remoteDir+`
					git push -u origin main
					git checkout -b feature
					git branch -D main
				`)
			},
			expectedBranch: "main",
		},
		{
			name: "detects remote master branch",
			setup: func(t *testing.T, repoPath string) {
				remoteDir := filepath.Join(t.TempDir(), "remote.git")
				testutil.ExecOrFail(t, repoPath, `
					git init --bare `+remoteDir+`
					git init
					git checkout -b master
					git config user.email "test@example.com"
					git config user.name "Test User"
					git commit --allow-empty -m "initial commit"
					git remote add origin `+remoteDir+`
					git push -u origin master
					git checkout -b feature
					git branch -D master
				`)
			},
			expectedBranch: "master",
		},
		{
			name: "returns empty string when no main/master branch exists",
			setup: func(t *testing.T, repoPath string) {
				testutil.ExecOrFail(t, repoPath, `
					git init
					git checkout -b develop
					git config user.email "test@example.com"
					git config user.name "Test User"
					git commit --allow-empty -m "initial commit"
				`)
			},
			expectedBranch: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repoPath := t.TempDir()
			tt.setup(t, repoPath)

			repo := WithRepo(repoPath)
			branch, err := repo.DetectMainBranch()
			assert.NilError(t, err)
			assert.Equal(t, tt.expectedBranch, branch)
		})
	}
}

func TestGetRemoteForBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setup          func(t *testing.T, repoPath string)
		branchNames    []string
		expectedRemote string
		expectError    bool
		errorContains  string
	}{
		{
			name: "returns remote for branch with configured remote",
			setup: func(t *testing.T, repoPath string) {
				testutil.ExecOrFail(t, repoPath, `
					git init
					git config user.email "test@example.com"
					git config user.name "Test User"
					git commit --allow-empty -m "initial commit"
					git checkout -b feature
					git remote add origin https://github.com/user/repo.git
					git config branch.feature.remote origin
				`)
			},
			branchNames:    []string{"feature"},
			expectedRemote: "origin",
			expectError:    false,
		},
		{
			name: "returns remote for first branch with configured remote",
			setup: func(t *testing.T, repoPath string) {
				testutil.ExecOrFail(t, repoPath, `
					git init
					git config user.email "test@example.com"
					git config user.name "Test User"
					git commit --allow-empty -m "initial commit"
					git checkout -b feature1
					git checkout -b feature2
					git remote add origin https://github.com/user/repo.git
					git remote add upstream https://github.com/upstream/repo.git
					git config branch.feature1.remote origin
					git config branch.feature2.remote upstream
				`)
			},
			branchNames:    []string{"feature1", "feature2"},
			expectedRemote: "origin",
			expectError:    false,
		},
		{
			name: "returns remote for second branch when first has no remote",
			setup: func(t *testing.T, repoPath string) {
				testutil.ExecOrFail(t, repoPath, `
					git init
					git config user.email "test@example.com"
					git config user.name "Test User"
					git commit --allow-empty -m "initial commit"
					git checkout -b feature1
					git checkout -b feature2
					git remote add upstream https://github.com/upstream/repo.git
					git config branch.feature2.remote upstream
				`)
			},
			branchNames:    []string{"feature1", "feature2"},
			expectedRemote: "upstream",
			expectError:    false,
		},
		{
			name: "falls back to the only remote when no branch has one configured",
			setup: func(t *testing.T, repoPath string) {
				testutil.ExecOrFail(t, repoPath, `
					git init
					git config user.email "test@example.com"
					git config user.name "Test User"
					git commit --allow-empty -m "initial commit"
					git checkout -b feature
					git remote add upstream https://github.com/upstream/repo.git
				`)
			},
			branchNames:    []string{"feature"},
			expectedRemote: "upstream",
			expectError:    false,
		},
		{
			name: "returns error when no branch names provided",
			setup: func(t *testing.T, repoPath string) {
				testutil.ExecOrFail(t, repoPath, `
					git init
					git config user.email "test@example.com"
					git config user.name "Test User"
					git commit --allow-empty -m "initial commit"
				`)
			},
			branchNames:    []string{},
			expectedRemote: "",
			expectError:    true,
			errorContains:  "no branch names provided",
		},
		{
			name: "handles custom remote names",
			setup: func(t *testing.T, repoPath string) {
				testutil.ExecOrFail(t, repoPath, `
					git init
					git config user.email "test@example.com"
					git config user.name "Test User"
					git commit --allow-empty -m "initial commit"
					git checkout -b feature
					git remote add custom-remote https://github.com/user/repo.git
					git config branch.feature.remote custom-remote
				`)
			},
			branchNames:    []string{"feature"},
			expectedRemote: "custom-remote",
			expectError:    false,
		},
		{
			name: "prefers branch.<name>.pushRemote over the branch's remote",
			setup: func(t *testing.T, repoPath string) {
				testutil.ExecOrFail(t, repoPath, `
					git init
					git config user.email "test@example.com"
					git config user.name "Test User"
					git commit --allow-empty -m "initial commit"
					git checkout -b feature
					git remote add origin https://github.com/user/repo.git
					git remote add fork https://github.com/user/fork.git
					git config branch.feature.remote origin
					git config branch.feature.pushRemote fork
				`)
			},
			branchNames:    []string{"feature"},
			expectedRemote: "fork",
			expectError:    false,
		},
		{
			name: "prefers remote.pushDefault over the branch's remote",
			setup: func(t *testing.T, repoPath string) {
				testutil.ExecOrFail(t, repoPath, `
					git init
					git config user.email "test@example.com"
					git config user.name "Test User"
					git commit --allow-empty -m "initial commit"
					git checkout -b feature
					git remote add origin https://github.com/user/repo.git
					git remote add fork https://github.com/user/fork.git
					git config branch.feature.remote origin
					git config remote.pushDefault fork
				`)
			},
			branchNames:    []string{"feature"},
			expectedRemote: "fork",
			expectError:    false,
		},
		{
			name: "falls back to origin when there are several remotes",
			setup: func(t *testing.T, repoPath string) {
				testutil.ExecOrFail(t, repoPath, `
					git init
					git config user.email "test@example.com"
					git config user.name "Test User"
					git commit --allow-empty -m "initial commit"
					git checkout -b feature
					git remote add fork https://github.com/user/fork.git
					git remote add origin https://github.com/user/repo.git
				`)
			},
			branchNames:    []string{"feature"},
			expectedRemote: "origin",
			expectError:    false,
		},
		{
			name: "returns error when there are several remotes and none is origin",
			setup: func(t *testing.T, repoPath string) {
				testutil.ExecOrFail(t, repoPath, `
					git init
					git config user.email "test@example.com"
					git config user.name "Test User"
					git commit --allow-empty -m "initial commit"
					git checkout -b feature
					git remote add fork https://github.com/user/fork.git
					git remote add upstream https://github.com/upstream/repo.git
				`)
			},
			branchNames:    []string{"feature"},
			expectedRemote: "",
			expectError:    true,
			errorContains:  "cannot determine which remote to use (fork, upstream): set remote.pushDefault",
		},
		{
			name: "returns error when there are no remotes",
			setup: func(t *testing.T, repoPath string) {
				testutil.ExecOrFail(t, repoPath, `
					git init
					git config user.email "test@example.com"
					git config user.name "Test User"
					git commit --allow-empty -m "initial commit"
					git checkout -b feature
					git config core.bare false
				`)
			},
			branchNames:    []string{"feature"},
			expectedRemote: "",
			expectError:    true,
			errorContains:  "repository has no remotes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repoPath := t.TempDir()
			tt.setup(t, repoPath)

			repo := WithRepo(repoPath)
			remote, err := repo.GetRemoteForBranch(tt.branchNames...)

			if tt.expectError {
				assert.Error(t, err, tt.errorContains)
				assert.Equal(t, "", remote)
			} else {
				assert.NilError(t, err)
				assert.Equal(t, tt.expectedRemote, remote)
			}
		})
	}
}

func TestLinkedWorktreesTolerateStalePath(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	worktreePath := filepath.Join(t.TempDir(), "stale-worktree")

	testutil.ExecOrFail(t, repoPath, `
		git init
		git checkout -b main
		git config user.email "test@example.com"
		git config user.name "Test User"
		git commit --allow-empty -m "initial commit"
		git branch feature
		git worktree add `+worktreePath+` feature
	`)

	// Simulate a stale/prunable worktree by removing its directory without
	// running `git worktree prune`. Git still lists the entry, but its path no
	// longer exists on disk.
	assert.NilError(t, os.RemoveAll(worktreePath))

	repo := WithRepo(repoPath)

	// LinkedWorktrees should not error on the missing path; the stale entry is
	// skipped.
	linked, err := repo.LinkedWorktrees()
	assert.NilError(t, err)
	assert.Equal(t, 0, len(linked))

	// Deleting an unrelated branch must not be blocked by the stale worktree.
	path, err := repo.LinkedWorktreePathForBranch("some-other-branch")
	assert.NilError(t, err)
	assert.Equal(t, "", path)
}
