package test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/stringutil"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

// upstreamOf returns the upstream ref of the given branch, or "" if it has none.
func upstreamOf(workingDir string, branchName string) string {
	if mustExecExitCode(workingDir, "git", "rev-parse", branchName+"@{upstream}") != 0 {
		return ""
	}

	return strings.TrimSpace(mustExecOutput(workingDir, "git", "rev-parse", "--abbrev-ref", branchName+"@{upstream}"))
}

func TestRefresh_ConfiguresUpstreamTracking(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := filepath.Join(t.TempDir(), "origin.git")

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{BaseRefName: "main"})

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		touch main
		git add main
		git commit -m "main-0"
		git push -u origin main

		# topic-a, pushed without setting upstream
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"
		git push origin topic-a
	`, map[string]string{"fakeOrigin": fakeOrigin}))

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())

	assert.Equal(t, "", upstreamOf(tempDir, "topic-a"), "branch should start with no upstream")

	assert.NilError(t, cli.Run("refresh", "topic-a").Err())

	assert.Equal(t, "origin/topic-a", upstreamOf(tempDir, "topic-a"))
}

func TestRefresh_FetchesRemoteBranchToConfigureUpstreamTracking(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := filepath.Join(t.TempDir(), "origin.git")

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{BaseRefName: "main"})

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		touch main
		git add main
		git commit -m "main-0"
		git push -u origin main

		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"
		git push origin topic-a

		# Simulate never having fetched the branch: the remote has it (and the PR
		# is open), but we have no remote-tracking ref for it locally.
		git update-ref -d refs/remotes/origin/topic-a
	`, map[string]string{"fakeOrigin": fakeOrigin}))

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())

	assert.NilError(t, cli.Run("refresh", "topic-a").Err())

	assert.Equal(t, "origin/topic-a", upstreamOf(tempDir, "topic-a"))
}

func TestRefresh_LeavesUpstreamUnsetWhenBranchIsNotOnRemote(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := filepath.Join(t.TempDir(), "origin.git")

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// No PR for this branch
	cli.Mock(
		"gh", "pr", "list",
		"--head", "topic-a",
		"--state", "all",
		"--json", "id,state,url,isDraft,baseRefName",
	).WithStdout("[]")

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		touch main
		git add main
		git commit -m "main-0"
		git push -u origin main

		# topic-a exists locally only
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"
	`, map[string]string{"fakeOrigin": fakeOrigin}))

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())

	assert.NilError(t, cli.Run("refresh", "topic-a").Err())

	// Tracking a branch that isn't on the remote would leave it in git's
	// "upstream is gone" state, so it must be left alone.
	assert.Equal(t, "", upstreamOf(tempDir, "topic-a"))
}
