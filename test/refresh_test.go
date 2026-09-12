package test

import (
	"path/filepath"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/stringutil"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

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

	mockNoGitHubPRForBranch(cli, "topic-a")

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

// gh derives the GitHub repository from its working directory, so when yas is
// pointed at a repository via --repo, gh has to run inside that repository
// rather than wherever yas was invoked from.
func TestRefresh_RunsGhInsideSelectedRepo(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	outsideDir := t.TempDir()
	fakeOrigin := filepath.Join(t.TempDir(), "origin.git")

	// Invoke yas from a directory that is not a git repository at all.
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(outsideDir),
	)

	ghPRList := cli.Mock(
		"gh", "pr", "list",
		"--head", "topic-a",
		"--state", "all",
		"--json", "id,state,url,isDraft,baseRefName",
	).WithStdout(mustMarshalJSON([]yas.PullRequestMetadata{{BaseRefName: "main"}}))

	testutil.ExecOrFail(t, repoDir, stringutil.MustInterpolate(`
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
		git push -u origin topic-a
	`, map[string]string{"fakeOrigin": fakeOrigin}))

	assert.NilError(t, cli.Run("--repo="+repoDir, "config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("--repo="+repoDir, "refresh", "topic-a").Err())

	calls := ghPRList.Calls()
	assert.Assert(t, len(calls) > 0, "expected gh pr list to be called")

	for _, call := range calls {
		assert.Equal(t, resolvePath(call.Dir), resolvePath(repoDir))
	}
}
