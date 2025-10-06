package gitexec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"gotest.tools/v3/assert"
)

func TestDetectMainBranch(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T, repoPath string)
		expectedBranch string
	}{
		{
			name: "detects local main branch",
			setup: func(t *testing.T, repoPath string) {
				testutil.ExecOrFail(t, `
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
				testutil.ExecOrFail(t, `
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
				testutil.ExecOrFail(t, `
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
				testutil.ExecOrFail(t, `
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
				testutil.ExecOrFail(t, `
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
				testutil.ExecOrFail(t, `
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
			testutil.WithTempWorkingDir(t, func() {
				repoPath, err := os.Getwd()
				assert.NilError(t, err)

				tt.setup(t, repoPath)

				repo := WithRepo(repoPath)
				branch, err := repo.DetectMainBranch()
				assert.NilError(t, err)
				assert.Equal(t, tt.expectedBranch, branch)
			})
		})
	}
}
