package test

import (
	"testing"

	"dario.cat/mergo"
	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/stringutil"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

var defaultPRMetadata = yas.PullRequestMetadata{
	ID:          "fakeid",
	State:       "OPEN",
	URL:         "https://github.com/test/test/pull/42",
	IsDraft:     false,
	BaseRefName: "main",
}

func mustMerge(dst, src any) {
	if err := mergo.MergeWithOverwrite(dst, src); err != nil {
		panic(err)
	}
}

func githubPRURL(prNumber string) string {
	return "https://github.com/test/test/pull/" + prNumber
}

// mockGitHubPRForBranch creates a mock for the "gh" command to return the specified PR metadata for the given branch.
func mockGitHubPRForBranch(cli *gocmdtester.CmdTester, branchName string, prMetadataOverrides yas.PullRequestMetadata) {
	prMetadata := yas.PullRequestMetadata{}
	mustMerge(&prMetadata, defaultPRMetadata)
	mustMerge(&prMetadata, prMetadataOverrides)

	cli.Mock(
		"gh", "pr", "list",
		"--head", branchName,
		"--state", "all",
		"--json", "id,state,url,isDraft,baseRefName",
	).WithStdout(mustMarshalJSON([]yas.PullRequestMetadata{prMetadata}))
}

func TestAnnotate_UpdatesPRWithStackInfo(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{URL: githubPRURL("41"), BaseRefName: "main"})
	mockGitHubPRForBranch(cli, "topic-b", yas.PullRequestMetadata{URL: githubPRURL("42"), BaseRefName: "topic-a"})
	mockGitHubPRForBranch(cli, "topic-c", yas.PullRequestMetadata{URL: githubPRURL("43"), BaseRefName: "topic-b"})

	// Expect annotate command to fetch the PR body
	cli.Mock("gh", "pr", "view", "42", "--json", "body", "-q", ".body").WithStdout("Foo body")

	// Expect annotate to update the PR body with the stack information
	cli.Mock("gh", "pr", "edit", "42", "--body", `Foo body

---

Stacked PRs:

* https://github.com/test/test/pull/41
  * https://github.com/test/test/pull/42 👈 (this PR)
    * https://github.com/test/test/pull/43`)

	fakeOrigin := t.TempDir()

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		# Set up "remote" repository
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# topic-b (child of topic-a)
		git checkout -b topic-b
		touch b
		git add b
		git commit -m "topic-b-0"

		# topic-c (child of topic-b)
		git checkout -b topic-c
		touch c
		git add c
		git commit -m "topic-c-0"

		git push origin topic-c
	`, map[string]string{
		"fakeOrigin": fakeOrigin,
	}))

	// Initialize yas config
	_, err := yas.WriteConfig(yas.Config{
		RepoDirectory: tempDir,
		TrunkBranch:   "main",
	})
	assert.NilError(t, err)

	// Set up branches
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())
	assert.NilError(t, cli.Run("add", "topic-c", "--parent=topic-b").Err())

	// Refresh branches from remote
	assert.NilError(t, cli.Run("refresh", "topic-a", "topic-b", "topic-c").Err())

	// Submit topic-b to create a PR
	testutil.ExecOrFail(t, tempDir, "git checkout topic-b")

	assert.NilError(t, cli.Run("submit").Err())
	assert.NilError(t, cli.Run("annotate").Err())
}

func TestAnnotate_SinglePRInStack_DoesNotAddStackSection(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{URL: githubPRURL("42"), BaseRefName: "main"})

	// Expect annotate command to fetch the PR body
	cli.Mock("gh", "pr", "view", "42", "--json", "body", "-q", ".body").WithStdout("This is my PR description.")

	// For a single PR in stack, annotate should call gh pr edit with the same body (no stack section added)
	cli.Mock("gh", "pr", "edit", "42", "--body", "This is my PR description.")

	fakeOrigin := t.TempDir()

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		# Set up "remote" repository
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a (only PR in stack)
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		git push origin topic-a
	`, map[string]string{
		"fakeOrigin": fakeOrigin,
	}))

	// Initialize yas config
	_, err := yas.WriteConfig(yas.Config{
		RepoDirectory: tempDir,
		TrunkBranch:   "main",
	})
	assert.NilError(t, err)

	// Set up branch
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Refresh branch from remote
	assert.NilError(t, cli.Run("refresh", "topic-a").Err())

	// Switch to topic-a
	testutil.ExecOrFail(t, tempDir, "git checkout topic-a")

	// Submit to create a PR
	assert.NilError(t, cli.Run("submit").Err())

	// Run annotate - should not add stack section for single PR
	assert.NilError(t, cli.Run("annotate").Err())
}

func TestAnnotate_SinglePRInStack_RemovesExistingStackSection(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{URL: githubPRURL("42"), BaseRefName: "main"})

	// PR body has an existing stack section that should be removed for a single PR
	existingBody := `This is my PR description.

---

Stacked PRs:

* https://github.com/test/test/pull/42 👈 (this PR)`

	// Expect annotate command to fetch the PR body
	cli.Mock("gh", "pr", "view", "42", "--json", "body", "-q", ".body").WithStdout(existingBody)

	// For a single PR in stack, annotate should remove the stack section and update with just the description
	cli.Mock("gh", "pr", "edit", "42", "--body", "This is my PR description.")

	fakeOrigin := t.TempDir()

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		# Set up "remote" repository
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		# main
		touch main
		git add main
		git commit -m "main-0"

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		git push origin topic-a
	`, map[string]string{
		"fakeOrigin": fakeOrigin,
	}))

	// Initialize yas config
	_, err := yas.WriteConfig(yas.Config{
		RepoDirectory: tempDir,
		TrunkBranch:   "main",
	})
	assert.NilError(t, err)

	// Set up branch
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Refresh branch from remote
	assert.NilError(t, cli.Run("refresh", "topic-a").Err())

	// Switch to topic-a
	testutil.ExecOrFail(t, tempDir, "git checkout topic-a")

	// Submit to create a PR - this calls annotate internally which should remove the stack section
	assert.NilError(t, cli.Run("submit").Err())
}
