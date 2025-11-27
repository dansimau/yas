package test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/stringutil"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

func TestMerge_FailsWhenRestackInProgress(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// No mocks needed - merge fails before any gh calls
	cli.SkipMockVerification()

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		git remote add origin https://fake.origin/test/test.git

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
	`)

	// Initialize yas config and track branch
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Simulate a restack in progress
	restackState := &yas.RestackState{
		StartingBranch: "topic-a",
		CurrentBranch:  "topic-a",
		CurrentParent:  "main",
	}
	err := yas.SaveRestackState(tempDir, restackState)
	assert.NilError(t, err)

	// Try to merge - should fail
	result := cli.Run("merge")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("restack operation is already in progress"))
}

func TestMerge_FailsWhenNoPR(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// No mocks needed - merge fails checking stored metadata (no PR ID stored)
	cli.SkipMockVerification()

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		git remote add origin https://fake.origin/test/test.git

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
	`)

	// Initialize yas config and track branch (but don't submit - no PR)
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Try to merge - should fail
	result := cli.Run("merge")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("does not have a PR"))
}

func TestMerge_FailsWhenNotAtTopOfStack(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Mock PR info for topic-b (needed to have a PR ID stored)
	mockGitHubPRForBranch(cli, "topic-b", yas.PullRequestMetadata{
		ID:          "PR_kwDOTest123b",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/42",
		BaseRefName: "topic-a", // Parent is topic-a, not main
	})

	// Skip verification - merge fails before calling all mocks
	cli.SkipMockVerification()

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		git remote add origin https://fake.origin/test/test.git

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
	`)

	// Initialize yas config and track branches
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())

	// Refresh to get PR metadata (stores PR ID in metadata)
	assert.NilError(t, cli.Run("refresh", "topic-b").Err())

	// Try to merge topic-b (which is not at top of stack) - should fail
	result := cli.Run("merge")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("must be at top of stack"))
	assert.Assert(t, result.StderrContains("Merge parent branches first"))
}

func TestMerge_FailsWhenNeedsRestack(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Mock PR info for topic-a (needed to have a PR ID stored)
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		ID:          "PR_kwDOTest123",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/42",
		BaseRefName: "main",
	})

	// Skip verification - merge fails before calling all mocks
	cli.SkipMockVerification()

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		git remote add origin https://fake.origin/test/test.git

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

		# Add a commit to main (will make topic-a need restack)
		git checkout main
		touch main2
		git add main2
		git commit -m "main-1"

		git checkout topic-a
	`)

	// Initialize yas config and track branch
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Refresh to get PR metadata (stores PR ID in metadata)
	assert.NilError(t, cli.Run("refresh", "topic-a").Err())

	// Try to merge - should fail because branch needs restack
	result := cli.Run("merge")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("branch needs restack"))
}

func TestMerge_FailsWhenCINotPassing(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Mock basic PR info (used by refresh)
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		ID:          "PR_kwDOTest123",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/42",
		BaseRefName: "main",
	})

	// Mock extended PR query for merge checks (with failing CI)
	cli.Mock(
		"gh", "pr", "list",
		"--head", "topic-a",
		"--state", "all",
		"--json", "id,state,url,isDraft,baseRefName,reviewDecision,statusCheckRollup",
	).WithStdout(mustMarshalJSON([]yas.PullRequestMetadata{{
		ID:                "PR_kwDOTest123",
		State:             "OPEN",
		URL:               "https://github.com/test/test/pull/42",
		BaseRefName:       "main",
		ReviewDecision:    "APPROVED",
		StatusCheckRollup: []yas.StatusCheck{{State: "FAILURE", Conclusion: "FAILURE"}},
	}}))

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

		# Push topic-a to simulate submitted state
		git push -u origin topic-a
	`, map[string]string{
		"fakeOrigin": fakeOrigin,
	}))

	// Initialize yas config and track branch
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Refresh to populate PR metadata
	assert.NilError(t, cli.Run("refresh", "topic-a").Err())

	// Try to merge - should fail because CI is failing
	result := cli.Run("merge")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("CI checks are not passing"))
	assert.Assert(t, result.StderrContains("Use --force to override"))
}

func TestMerge_FailsWhenNotApproved(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Mock basic PR info (used by refresh)
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		ID:          "PR_kwDOTest123",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/42",
		BaseRefName: "main",
	})

	// Mock extended PR query for merge checks (without approval)
	cli.Mock(
		"gh", "pr", "list",
		"--head", "topic-a",
		"--state", "all",
		"--json", "id,state,url,isDraft,baseRefName,reviewDecision,statusCheckRollup",
	).WithStdout(mustMarshalJSON([]yas.PullRequestMetadata{{
		ID:             "PR_kwDOTest123",
		State:          "OPEN",
		URL:            "https://github.com/test/test/pull/42",
		BaseRefName:    "main",
		ReviewDecision: "REVIEW_REQUIRED",
	}}))

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

		# Push topic-a to simulate submitted state
		git push -u origin topic-a
	`, map[string]string{
		"fakeOrigin": fakeOrigin,
	}))

	// Initialize yas config and track branch
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Refresh to populate PR metadata
	assert.NilError(t, cli.Run("refresh", "topic-a").Err())

	// Try to merge - should fail because PR needs approval
	result := cli.Run("merge")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("PR needs approval"))
	assert.Assert(t, result.StderrContains("Use --force to override"))
}

func TestMerge_SucceedsWithForceFlag(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := t.TempDir()

	// Set up EDITOR to auto-approve merge message (appends newline + comment)
	// Using printf to properly add a newline before the comment
	editorScript := filepath.Join(tempDir, "editor.sh")
	err := os.WriteFile(editorScript, []byte("#!/bin/bash\nprintf '\\n# User edited merge message' >> \"$1\"\n"), 0o755)
	assert.NilError(t, err)

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("EDITOR", editorScript),
	)

	// Mock basic PR info (used by refresh)
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		ID:          "PR_kwDOTest123",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/42",
		BaseRefName: "main",
	})

	// Mock gh pr view for merge (to get title and body)
	cli.Mock("gh", "pr", "view", "42", "--json", "title,body", "-q", ".title + \"\n---SEPARATOR---\n\" + .body").WithStdout("Test PR Title\n---SEPARATOR---\nTest PR Body")

	// Mock gh pr merge command - body will have original + newline + comment
	cli.Mock("gh", "pr", "merge", "42", "--squash", "--auto", "--subject", "Test PR Title", "--body", "Test PR Body\n# User edited merge message")

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

		# Push topic-a to simulate submitted state
		git push -u origin topic-a
	`, map[string]string{
		"fakeOrigin": fakeOrigin,
	}))

	// Initialize yas config and track branch
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Refresh to populate PR metadata
	assert.NilError(t, cli.Run("refresh", "topic-a").Err())

	// Try to merge with --force - should succeed even with failing CI/reviews
	result := cli.Run("merge", "--force")
	assert.NilError(t, result.Err())

	// Verify merge message file was cleaned up
	mergeFilePath := filepath.Join(tempDir, ".git", "yas-merge-msg")
	_, err = os.Stat(mergeFilePath)
	assert.Assert(t, os.IsNotExist(err), "merge message file should be cleaned up")
}

func TestMerge_AbortsWhenMergeMessageEmpty(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := t.TempDir()

	// Set up EDITOR to clear the file (simulating user deleting content)
	editorScript := filepath.Join(tempDir, "editor.sh")
	err := os.WriteFile(editorScript, []byte("#!/bin/bash\necho \"\" > \"$1\"\n"), 0o755)
	assert.NilError(t, err)

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("EDITOR", editorScript),
	)

	// Mock basic PR info (used by refresh)
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		ID:          "PR_kwDOTest123",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/42",
		BaseRefName: "main",
	})

	// Mock extended PR query for merge checks (approved, so we proceed to editor)
	cli.Mock(
		"gh", "pr", "list",
		"--head", "topic-a",
		"--state", "all",
		"--json", "id,state,url,isDraft,baseRefName,reviewDecision,statusCheckRollup",
	).WithStdout(mustMarshalJSON([]yas.PullRequestMetadata{{
		ID:             "PR_kwDOTest123",
		State:          "OPEN",
		URL:            "https://github.com/test/test/pull/42",
		BaseRefName:    "main",
		ReviewDecision: "APPROVED",
	}}))

	// Mock gh pr view for merge (to get title and body)
	cli.Mock("gh", "pr", "view", "42", "--json", "title,body", "-q", ".title + \"\n---SEPARATOR---\n\" + .body").WithStdout("Test PR Title\n---SEPARATOR---\nTest PR Body")

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

		# Push topic-a to simulate submitted state
		git push -u origin topic-a
	`, map[string]string{
		"fakeOrigin": fakeOrigin,
	}))

	// Initialize yas config and track branch
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// Refresh to populate PR metadata
	assert.NilError(t, cli.Run("refresh", "topic-a").Err())

	// Try to merge - should fail because merge message is empty
	result := cli.Run("merge")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("merge aborted"))
	assert.Assert(t, result.StderrContains("empty commit message"))

	// Verify merge message file was cleaned up even after abort
	mergeFilePath := filepath.Join(tempDir, ".git", "yas-merge-msg")
	_, err = os.Stat(mergeFilePath)
	assert.Assert(t, os.IsNotExist(err), "merge message file should be cleaned up after abort")
}

func TestMerge_WithBranchNameReturnsToOriginalBranch(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := t.TempDir()

	// Set up EDITOR to auto-approve merge message (appends newline + comment)
	// Using printf to properly add a newline before the comment
	editorScript := filepath.Join(tempDir, "editor.sh")
	err := os.WriteFile(editorScript, []byte("#!/bin/bash\nprintf '\\n# User edited merge message' >> \"$1\"\n"), 0o755)
	assert.NilError(t, err)

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("EDITOR", editorScript),
	)

	// Mock basic PR info for topic-a (used by refresh)
	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{
		ID:          "PR_kwDOTest123",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/42",
		BaseRefName: "main",
	})

	// Mock gh pr view for merge (to get title and body)
	cli.Mock("gh", "pr", "view", "42", "--json", "title,body", "-q", ".title + \"\n---SEPARATOR---\n\" + .body").WithStdout("Test PR Title\n---SEPARATOR---\nTest PR Body")

	// Mock gh pr merge command - body will have original + newline + comment
	cli.Mock("gh", "pr", "merge", "42", "--squash", "--auto", "--subject", "Test PR Title", "--body", "Test PR Body\n# User edited merge message")

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

		# Push topic-a to simulate submitted state
		git push -u origin topic-a

		# other-branch (we'll be on this branch when merging topic-a)
		git checkout -b other-branch
		touch other
		git add other
		git commit -m "other-0"
	`, map[string]string{
		"fakeOrigin": fakeOrigin,
	}))

	// Initialize yas config and track branches
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "other-branch", "--parent=main").Err())

	// Refresh topic-a to populate PR metadata
	assert.NilError(t, cli.Run("refresh", "topic-a").Err())

	// Verify we're on other-branch
	currentBranch := mustExecOutput(tempDir, "git", "branch", "--show-current")
	equalLines(t, currentBranch, "other-branch")

	// Merge topic-a (specifying the branch name explicitly)
	result := cli.Run("merge", "topic-a", "--force")
	assert.NilError(t, result.Err())

	// Verify we're back on other-branch after the merge
	currentBranch = mustExecOutput(tempDir, "git", "branch", "--show-current")
	equalLines(t, currentBranch, "other-branch")
}

func TestMerge_SucceedsFromWorktree(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeOrigin := t.TempDir()

	// Set up EDITOR to auto-approve merge message
	editorScript := filepath.Join(tempDir, "editor.sh")
	err := os.WriteFile(editorScript, []byte("#!/bin/bash\nprintf '\\n# User edited merge message' >> \"$1\"\n"), 0o755)
	assert.NilError(t, err)

	// Create main repo with topic-a branch and a worktree for it
	worktreePath := filepath.Join(tempDir, "worktrees", "topic-a")
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

		# Push topic-a to simulate submitted state
		git push -u origin topic-a

		# Go back to main and create worktree for topic-a
		git checkout main
		git worktree add {{.worktreePath}} topic-a
	`, map[string]string{
		"fakeOrigin":   fakeOrigin,
		"worktreePath": worktreePath,
	}))

	// Initialize yas from main repo
	cliMain := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	// Mock basic PR info for topic-a (used by refresh)
	mockGitHubPRForBranch(cliMain, "topic-a", yas.PullRequestMetadata{
		ID:          "PR_kwDOTest123",
		State:       "OPEN",
		URL:         "https://github.com/test/test/pull/42",
		BaseRefName: "main",
	})

	assert.NilError(t, cliMain.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cliMain.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cliMain.Run("refresh", "topic-a").Err())

	// Now run merge from inside the worktree
	cliWorktree := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(worktreePath),
		gocmdtester.WithEnv("EDITOR", editorScript),
	)

	// Mock gh pr view for merge (to get title and body)
	cliWorktree.Mock("gh", "pr", "view", "42", "--json", "title,body", "-q", ".title + \"\n---SEPARATOR---\n\" + .body").WithStdout("Test PR Title\n---SEPARATOR---\nTest PR Body")

	// Mock gh pr merge command
	cliWorktree.Mock("gh", "pr", "merge", "42", "--squash", "--auto", "--subject", "Test PR Title", "--body", "Test PR Body\n# User edited merge message")

	// This tests the bug fix: merge should succeed from worktree
	// Previously it would fail with "not a directory" when trying to write to .git/yas-merge-msg
	// because .git is a file in worktrees, not a directory
	result := cliWorktree.Run("merge", "--force")
	assert.NilError(t, result.Err())

	// Verify merge message file was written to .yas directory (not .git)
	// and was cleaned up after merge
	mergeFilePath := filepath.Join(tempDir, ".yas", "yas-merge-msg")
	_, err = os.Stat(mergeFilePath)
	assert.Assert(t, os.IsNotExist(err), "merge message file should be cleaned up")
}
