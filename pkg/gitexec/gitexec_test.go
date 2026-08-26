package gitexec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"gotest.tools/v3/assert"
)

// setupRepo creates a repository with a single commit, plus any extra git
// commands the test needs.
func setupRepo(t *testing.T, repoPath string, gitCommands string) {
	testutil.ExecOrFail(t, repoPath, `
		git init
		git config user.email "test@example.com"
		git config user.name "Test User"
		git commit --allow-empty -m "initial commit"
	`+gitCommands)
}

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

func TestPushRemoteConfigRemoteFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		gitCommands    string
		branchName     string
		expectedRemote string
	}{
		{
			name: "uses the branch's remote",
			gitCommands: `
				git remote add origin https://github.com/user/repo.git
				git config branch.feature.remote origin
			`,
			branchName:     "feature",
			expectedRemote: "origin",
		},
		{
			name: "prefers branch.<name>.pushRemote over the branch's remote",
			gitCommands: `
				git config branch.feature.remote origin
				git config branch.feature.pushRemote fork
			`,
			branchName:     "feature",
			expectedRemote: "fork",
		},
		{
			name: "prefers remote.pushDefault over the branch's remote",
			gitCommands: `
				git config branch.feature.remote origin
				git config remote.pushDefault fork
			`,
			branchName:     "feature",
			expectedRemote: "fork",
		},
		{
			name: "prefers branch.<name>.pushRemote over remote.pushDefault",
			gitCommands: `
				git config remote.pushDefault fork
				git config branch.feature.pushRemote other
			`,
			branchName:     "feature",
			expectedRemote: "other",
		},
		{
			name: "handles branch names containing dots and capitals",
			gitCommands: `
				git config branch.Feature.X.remote custom-remote
			`,
			branchName:     "Feature.X",
			expectedRemote: "custom-remote",
		},
		{
			name: "returns nothing when only another branch is configured",
			gitCommands: `
				git config branch.other.remote upstream
			`,
			branchName:     "feature",
			expectedRemote: "",
		},
		{
			name:           "returns nothing when nothing is configured",
			gitCommands:    "",
			branchName:     "feature",
			expectedRemote: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repoPath := t.TempDir()
			setupRepo(t, repoPath, tt.gitCommands)

			cfg, err := WithRepo(repoPath).PushRemoteConfig()
			assert.NilError(t, err)
			assert.Equal(t, tt.expectedRemote, cfg.RemoteFor(tt.branchName))
		})
	}
}

func TestDefaultRemote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		gitCommands    string
		expectedRemote string
		errorContains  string
	}{
		{
			name: "uses the only remote, whatever it is called",
			gitCommands: `
				git remote add upstream https://github.com/upstream/repo.git
			`,
			expectedRemote: "upstream",
		},
		{
			name: "falls back to origin when there are several remotes",
			gitCommands: `
				git remote add fork https://github.com/user/fork.git
				git remote add origin https://github.com/user/repo.git
			`,
			expectedRemote: "origin",
		},
		{
			name: "errors when there are several remotes and none is origin",
			gitCommands: `
				git remote add fork https://github.com/user/fork.git
				git remote add upstream https://github.com/upstream/repo.git
			`,
			errorContains: "cannot determine which remote to use (fork, upstream): set remote.pushDefault",
		},
		{
			name:          "errors when there are no remotes",
			gitCommands:   "",
			errorContains: "repository has no remotes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repoPath := t.TempDir()
			setupRepo(t, repoPath, tt.gitCommands)

			remote, err := WithRepo(repoPath).DefaultRemote()

			if tt.errorContains != "" {
				assert.Error(t, err, tt.errorContains)
				assert.Equal(t, "", remote)

				return
			}

			assert.NilError(t, err)
			assert.Equal(t, tt.expectedRemote, remote)
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
