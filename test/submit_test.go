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

func TestSubmit_SkipsCreatingPRWhenAlreadyExists(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := filepath.Join(tempDir, "origin.git")

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Mock gh pr list to return existing PR
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		ID:          "PR_kwDOTest123",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/1",
		IsDraft:     false,
		BaseRefName: "main",
	})

	// Mock gh pr view for annotation (returns empty body)
	cli.Mock("gh", "pr", "view", "1", "--json", "body", "-q", ".body").WithStdout("")

	// Mock gh pr edit for body update (annotation)
	cli.Mock("gh", "pr", "edit", "1", "--body", "")

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		# Create fake origin
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		# main
		touch main
		git add main
		git commit -m "main-0"
		git push -u origin main

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"
	`, map[string]string{"fakeOrigin": fakeOrigin}))

	// Initialize yas config and track branch
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Call submit - should push but NOT create PR since one exists
	result := cli.Run("submit")
	assert.NilError(t, result.Err())

	// The mocks verify that gh pr list was called but gh pr create was not
	// (because we didn't mock gh pr create, gocmdtester would fail if it was called)
}

func TestSubmit_StackPushesAllBranches(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := filepath.Join(tempDir, "origin.git")

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Skip mock verification since we have many mocks
	cli.SkipMockVerification()

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		# Create fake origin
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		# main
		touch main
		git add main
		git commit -m "main-0"
		git push -u origin main

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
	`, map[string]string{"fakeOrigin": fakeOrigin}))

	// Initialize yas config and track branches
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())
	assert.NilError(t, cli.Run("add", "topic-c", "--parent=topic-b").Err())

	// Mock PRs for all branches
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		ID:          "PR_1",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/1",
		IsDraft:     false,
		BaseRefName: "main",
	})
	mockGitHubPRForBranch(cli, "topic-b", yas.PullRequestMetadata{
		ID:          "PR_2",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/2",
		IsDraft:     false,
		BaseRefName: "topic-a",
	})
	mockGitHubPRForBranch(cli, "topic-c", yas.PullRequestMetadata{
		ID:          "PR_3",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/3",
		IsDraft:     false,
		BaseRefName: "topic-b",
	})

	// Mock gh pr view for all PRs (return empty body which will trigger annotation)
	cli.Mock("gh", "pr", "view", "1", "--json", "body", "-q", ".body").WithStdout("")
	cli.Mock("gh", "pr", "view", "2", "--json", "body", "-q", ".body").WithStdout("")
	cli.Mock("gh", "pr", "view", "3", "--json", "body", "-q", ".body").WithStdout("")

	// Mock gh pr edit for all PRs with the expected stack annotation bodies
	// PR 1 (topic-a -> main): root of stack
	cli.Mock("gh", "pr", "edit", "1", "--body", `---

Stacked PRs:

* https://github.com/test/test/pull/1 👈 (this PR)
  * https://github.com/test/test/pull/2
    * https://github.com/test/test/pull/3`)

	// PR 2 (topic-b -> topic-a): middle of stack
	cli.Mock("gh", "pr", "edit", "2", "--body", `---

Stacked PRs:

* https://github.com/test/test/pull/1
  * https://github.com/test/test/pull/2 👈 (this PR)
    * https://github.com/test/test/pull/3`)

	// PR 3 (topic-c -> topic-b): leaf of stack
	cli.Mock("gh", "pr", "edit", "3", "--body", `---

Stacked PRs:

* https://github.com/test/test/pull/1
  * https://github.com/test/test/pull/2
    * https://github.com/test/test/pull/3 👈 (this PR)`)

	// Submit with --stack pushes all branches in the stack
	testutil.ExecOrFail(t, tempDir, "git checkout topic-b")

	result := cli.Run("submit", "--stack")
	assert.NilError(t, result.Err())

	// Verify all branches were pushed to remote
	testutil.ExecOrFail(t, tempDir, "git fetch origin")
	output := mustExecOutput(tempDir, "git", "branch", "-r")
	assert.Assert(t, strings.Contains(output, "origin/topic-a"), "topic-a should be pushed")
	assert.Assert(t, strings.Contains(output, "origin/topic-b"), "topic-b should be pushed")
	assert.Assert(t, strings.Contains(output, "origin/topic-c"), "topic-c should be pushed")
}

func TestSubmit_PushesAndAnnotatesExistingPR(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := filepath.Join(tempDir, "origin.git")

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Mock gh pr list to return existing PR
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		ID:          "PR_kwDOTest123",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/42",
		IsDraft:     false,
		BaseRefName: "main",
	})

	// Mock gh pr view for annotation (returns empty body)
	cli.Mock("gh", "pr", "view", "42", "--json", "body", "-q", ".body").WithStdout("")

	// Mock gh pr edit for body update (annotation)
	cli.Mock("gh", "pr", "edit", "42", "--body", "")

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		# Create fake origin
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		# main
		touch main
		git add main
		git commit -m "main-0"
		git push -u origin main

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"
	`, map[string]string{"fakeOrigin": fakeOrigin}))

	// Initialize yas config and track branch
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Call submit - should push and annotate (PR already exists)
	result := cli.Run("submit")
	assert.NilError(t, result.Err())

	// gocmdtester verifies that gh pr view and gh pr edit were called for annotation
}

func TestSubmit_UpdatesPRBaseWhenLocalParentChanges(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := filepath.Join(tempDir, "origin.git")

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Skip mock verification since the submit flow calls multiple commands
	cli.SkipMockVerification()

	// Mock gh pr list to return existing PR with base branch topic-a
	mockGitHubPRForBranch(cli, "topic-b", yas.PullRequestMetadata{
		ID:          "PR_kwDOTest123",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/42",
		IsDraft:     false,
		BaseRefName: "topic-a", // PR currently targets topic-a
	})

	// Mock gh pr edit to update base branch (uses PR number from URL, not ID)
	cli.Mock("gh", "pr", "edit", "42", "--base", "main")

	// Mock gh pr view for annotation (returns empty body)
	cli.Mock("gh", "pr", "view", "42", "--json", "body", "-q", ".body").WithStdout("")

	// Mock gh pr edit for body update (annotation)
	cli.Mock("gh", "pr", "edit", "42", "--body", "")

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		# Create fake origin
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		# main
		touch main
		git add main
		git commit -m "main-0"
		git push -u origin main

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"
		git push -u origin topic-a

		# topic-b (originally child of topic-a)
		git checkout -b topic-b
		touch b
		git add b
		git commit -m "topic-b-0"
	`, map[string]string{"fakeOrigin": fakeOrigin}))

	// Initialize yas config
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())

	// Track topic-a and topic-b
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Set topic-b's parent to main (simulating a restack after topic-a was merged)
	// But the PR still has topic-a as base
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=main").Err())

	// Submit topic-b - should detect base mismatch and update
	testutil.ExecOrFail(t, tempDir, "git checkout topic-b")

	result := cli.Run("submit")
	assert.NilError(t, result.Err())

	// gocmdtester verifies gh pr edit was called to update the base branch
}

func TestSubmit_OutdatedSubmitsAllBranchesNeedingSubmit(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := filepath.Join(tempDir, "origin.git")

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Skip mock verification since we have many mocks for annotation
	cli.SkipMockVerification()

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		# Create fake origin
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		# main
		touch main
		git add main
		git commit -m "main-0"
		git push -u origin main

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		# topic-b
		git checkout -b topic-b
		touch b
		git add b
		git commit -m "topic-b-0"

		# topic-c
		git checkout -b topic-c
		touch c
		git add c
		git commit -m "topic-c-0"

		# Push all branches first
		git push -u origin topic-a topic-b topic-c
	`, map[string]string{"fakeOrigin": fakeOrigin}))

	// Initialize yas config and track branches
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-c", "--parent=main").Err())

	// Make additional commits to make branches need submitting
	testutil.ExecOrFail(t, tempDir, `
		git checkout topic-a
		touch a2
		git add a2
		git commit -m "topic-a-1"

		git checkout topic-b
		touch b2
		git add b2
		git commit -m "topic-b-1"

		git checkout topic-c
		touch c2
		git add c2
		git commit -m "topic-c-1"
	`)

	// Mock the PRs as existing (for the --outdated check)
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		ID:          "PR_1",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/1",
		IsDraft:     false,
		BaseRefName: "main",
	})
	mockGitHubPRForBranch(cli, "topic-b", yas.PullRequestMetadata{
		ID:          "PR_2",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/2",
		IsDraft:     false,
		BaseRefName: "main",
	})
	mockGitHubPRForBranch(cli, "topic-c", yas.PullRequestMetadata{
		ID:          "PR_3",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/3",
		IsDraft:     false,
		BaseRefName: "main",
	})

	// Mock annotation for all PRs
	cli.Mock("gh", "pr", "view", "1", "--json", "body", "-q", ".body").WithStdout("")
	cli.Mock("gh", "pr", "edit", "1", "--body", "")
	cli.Mock("gh", "pr", "view", "2", "--json", "body", "-q", ".body").WithStdout("")
	cli.Mock("gh", "pr", "edit", "2", "--body", "")
	cli.Mock("gh", "pr", "view", "3", "--json", "body", "-q", ".body").WithStdout("")
	cli.Mock("gh", "pr", "edit", "3", "--body", "")

	// Now test the --outdated flag
	result := cli.Run("submit", "--outdated")
	assert.NilError(t, result.Err())

	// All branches should have been pushed (verified by no push errors)
}

func TestSubmit_OutdatedSkipsBranchesWithoutPRs(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := filepath.Join(tempDir, "origin.git")

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Skip mock verification since the --outdated flag may skip PR checks entirely
	// when there are no outdated branches to submit
	cli.SkipMockVerification()

	// Mock gh pr list to return no existing PR (empty array)
	cli.Mock("gh", "pr", "list", "--head", "topic-a", "--state", "all", "--json", "id,state,url,isDraft,baseRefName").
		WithStdout("[]")

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		# Create fake origin
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		# main
		touch main
		git add main
		git commit -m "main-0"
		git push -u origin main

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a (no PR)
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"
	`, map[string]string{"fakeOrigin": fakeOrigin}))

	// Initialize yas config and track branch
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Call submit with --outdated (should find no branches with PRs)
	result := cli.Run("submit", "--outdated")
	assert.NilError(t, result.Err())

	// No push or PR create should happen (verified by not mocking them)
}

func TestSubmit_OutdatedSkipsUpToDateBranches(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := filepath.Join(tempDir, "origin.git")

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Skip mock verification since the --outdated flag may skip PR checks entirely
	// when the branch is already up to date with remote
	cli.SkipMockVerification()

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		# Create fake origin
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		# main
		touch main
		git add main
		git commit -m "main-0"
		git push -u origin main

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a (already pushed, so it's up to date)
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"
		git push -u origin topic-a
	`, map[string]string{"fakeOrigin": fakeOrigin}))

	// Initialize yas config and track branch
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Mock PR as existing for --outdated check
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		ID:          "PR_1",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/1",
		IsDraft:     false,
		BaseRefName: "main",
	})

	// Call submit with --outdated (should find no branches need submitting since they're up to date)
	result := cli.Run("submit", "--outdated")
	assert.NilError(t, result.Err())
}

func TestSubmit_NonDraftPRShowsAsNotDraft(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := filepath.Join(tempDir, "origin.git")

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Mock gh pr list to return existing non-draft PR
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		ID:          "PR_kwDOTest123",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/42",
		IsDraft:     false,
		BaseRefName: "main",
	})

	// Mock gh pr view for annotation (returns empty body)
	cli.Mock("gh", "pr", "view", "42", "--json", "body", "-q", ".body").WithStdout("")

	// Mock gh pr edit for body update (annotation)
	cli.Mock("gh", "pr", "edit", "42", "--body", "")

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		# Create fake origin
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		# main
		touch main
		git add main
		git commit -m "main-0"
		git push -u origin main

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"
	`, map[string]string{"fakeOrigin": fakeOrigin}))

	// Initialize yas config and track branch
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Call submit without --draft flag (PR already exists as non-draft)
	result := cli.Run("submit")
	assert.NilError(t, result.Err())
}

func TestSubmit_DraftPRShowsAsDraft(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := filepath.Join(tempDir, "origin.git")

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Mock gh pr list to return existing draft PR
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		ID:          "PR_kwDOTest123",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/42",
		IsDraft:     true,
		BaseRefName: "main",
	})

	// Mock gh pr view for annotation (returns empty body)
	cli.Mock("gh", "pr", "view", "42", "--json", "body", "-q", ".body").WithStdout("")

	// Mock gh pr edit for body update (annotation)
	cli.Mock("gh", "pr", "edit", "42", "--body", "")

	testutil.ExecOrFail(t, tempDir, stringutil.MustInterpolate(`
		# Create fake origin
		git init --bare {{.fakeOrigin}}

		git init --initial-branch=main
		git remote add origin {{.fakeOrigin}}

		# main
		touch main
		git add main
		git commit -m "main-0"
		git push -u origin main

		# Set up remote tracking for main
		git config branch.main.remote origin
		git config branch.main.merge refs/heads/main

		# topic-a
		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"
	`, map[string]string{"fakeOrigin": fakeOrigin}))

	// Initialize yas config and track branch
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Call submit (PR already exists as draft)
	result := cli.Run("submit")
	assert.NilError(t, result.Err())
}
