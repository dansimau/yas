package test

import (
	"strings"
	"testing"
	"time"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/stringutil"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

const stacksAPIPath = "repos/{owner}/{repo}/stacks"

// mockStacksUnavailable makes every Stacks API call report that stacked PRs are
// not enabled for the repository. Register it after any more specific `gh api`
// mock: the first matching mock wins.
func mockStacksUnavailable(cli *gocmdtester.CmdTester) *gocmdtester.Mock {
	return cli.Mock("gh", "api", gocmdtester.AnyFurtherArgs).
		WithCode(1).
		WithStderr("gh: Not Found (HTTP 404)\n")
}

// mockStackLookup mocks the lookup of the stack containing the given PR.
func mockStackLookup(cli *gocmdtester.CmdTester, prNumber string, stacks ...yas.Stack) *gocmdtester.Mock {
	if stacks == nil {
		stacks = []yas.Stack{}
	}

	return cli.Mock("gh", "api", stacksAPIPath+"?pull_request="+prNumber).
		WithStdout(mustMarshalJSON(stacks))
}

// mockGH mocks an exact `gh` invocation given as plain strings.
func mockGH(cli *gocmdtester.CmdTester, args ...string) *gocmdtester.Mock {
	anyArgs := make([]any, 0, len(args))
	for _, arg := range args {
		anyArgs = append(anyArgs, arg)
	}

	return cli.Mock("gh", anyArgs...)
}

func pullRequestFields(prNumbers ...string) []string {
	fields := []string{}
	for _, n := range prNumbers {
		fields = append(fields, "-F", "pull_requests[]="+n)
	}

	return fields
}

// mockStackCreate mocks the creation of a stack from the given PRs (bottom to
// top), responding with the given stack.
func mockStackCreate(cli *gocmdtester.CmdTester, response yas.Stack, prNumbers ...string) *gocmdtester.Mock {
	args := append([]string{"api", "--method", "POST", stacksAPIPath}, pullRequestFields(prNumbers...)...)

	return mockGH(cli, args...).WithStdout(mustMarshalJSON(response))
}

// mockStackAdd mocks appending PRs to the top of a stack.
func mockStackAdd(cli *gocmdtester.CmdTester, stackNumber string, prNumbers ...string) *gocmdtester.Mock {
	args := append([]string{"api", "--method", "POST", stacksAPIPath + "/" + stackNumber + "/add"}, pullRequestFields(prNumbers...)...)

	return mockGH(cli, args...).WithStdout("{}")
}

// mockStackUnstack mocks unstacking; an empty response means the stack was
// dissolved.
func mockStackUnstack(cli *gocmdtester.CmdTester, stackNumber string, response string) *gocmdtester.Mock {
	return cli.Mock("gh", "api", "--method", "POST", stacksAPIPath+"/"+stackNumber+"/unstack").WithStdout(response)
}

// annotationBody is the PR body yas writes for a stack of the given PRs, with
// the PR at index current marked as "this PR".
func annotationBody(prNumbers []string, current int) string {
	body := strings.Builder{}
	body.WriteString("---\n\nStacked PRs:\n")

	for i, n := range prNumbers {
		body.WriteString("\n" + strings.Repeat("  ", i) + "* " + githubPRURL(n))

		if i == current {
			body.WriteString(" 👈 (this PR)")
		}
	}

	return body.String()
}

func openStackPR(number int, head string) yas.StackPullRequest {
	return yas.StackPullRequest{Number: number, State: "open", Head: yas.StackRef{Ref: head}}
}

func mergedStackPR(number int, head string) yas.StackPullRequest {
	mergedAt := "2026-09-01T00:00:00Z"

	return yas.StackPullRequest{Number: number, State: "closed", MergedAt: &mergedAt, Head: yas.StackRef{Ref: head}}
}

func stackNumber7(prs ...yas.StackPullRequest) yas.Stack {
	return yas.Stack{ID: 1000, Number: 7, Base: yas.StackRef{Ref: "main"}, Open: true, PullRequests: prs}
}

func trackedBranchWithPR(name, parent, prNumber string) yas.BranchMetadata {
	return yas.BranchMetadata{
		Name:   name,
		Parent: parent,
		GitHubPullRequest: yas.PullRequestMetadata{
			ID:          "fakeid-" + name,
			State:       "OPEN",
			URL:         githubPRURL(prNumber),
			BaseRefName: parent,
		},
	}
}

// setupLinkRepo creates a repo with main and the branches topic-a, topic-b and
// topic-c stacked on each other (without pushing anything), seeds the yas state
// with the given branch metadata and checks out the given branch.
func setupLinkRepo(t *testing.T, cli *gocmdtester.CmdTester, tempDir string, branches map[string]yas.BranchMetadata, checkout string) {
	t.Helper()

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		touch main
		git add main
		git commit -m "main-0"

		git checkout -b topic-a
		touch a
		git add a
		git commit -m "topic-a-0"

		git checkout -b topic-b
		touch b
		git add b
		git commit -m "topic-b-0"

		git checkout -b topic-c
		touch c
		git add c
		git commit -m "topic-c-0"
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())

	writeStateFileToDir(t, tempDir, yasState{Branches: branches})

	testutil.ExecOrFail(t, tempDir, "git checkout "+checkout)
}

func twoBranchStack() map[string]yas.BranchMetadata {
	return map[string]yas.BranchMetadata{
		"topic-a": trackedBranchWithPR("topic-a", "main", "41"),
		"topic-b": trackedBranchWithPR("topic-b", "topic-a", "42"),
	}
}

func threeBranchStack() map[string]yas.BranchMetadata {
	branches := twoBranchStack()
	branches["topic-c"] = trackedBranchWithPR("topic-c", "topic-b", "43")

	return branches
}

func newLinkTester(t *testing.T) (*gocmdtester.CmdTester, string) {
	t.Helper()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	return cli, tempDir
}

func TestLink_CreatesStack(t *testing.T) {
	t.Parallel()

	cli, tempDir := newLinkTester(t)

	mockStackLookup(cli, "41")
	mockStackCreate(cli, stackNumber7(openStackPR(41, "topic-a"), openStackPR(42, "topic-b")), "41", "42")

	setupLinkRepo(t, cli, tempDir, twoBranchStack(), "topic-b")

	result := cli.Run("link")
	assert.NilError(t, result.Err())
	equalLines(t, result.Stdout(), "linked #41, #42 as stack #7")
}

func TestLink_AlreadyLinked(t *testing.T) {
	t.Parallel()

	cli, tempDir := newLinkTester(t)

	mockStackLookup(cli, "41", stackNumber7(openStackPR(41, "topic-a"), openStackPR(42, "topic-b")))

	setupLinkRepo(t, cli, tempDir, twoBranchStack(), "topic-b")

	result := cli.Run("link")
	assert.NilError(t, result.Err())
	equalLines(t, result.Stdout(), "already linked (stack #7)")
}

// Linking from the middle of a stack must not disturb the PRs stacked above.
func TestLink_AlreadyLinked_WhenStackExtendsAbove(t *testing.T) {
	t.Parallel()

	cli, tempDir := newLinkTester(t)

	mockStackLookup(cli, "41", stackNumber7(openStackPR(41, "topic-a"), openStackPR(42, "topic-b"), openStackPR(43, "topic-c")))

	setupLinkRepo(t, cli, tempDir, threeBranchStack(), "topic-b")

	result := cli.Run("link")
	assert.NilError(t, result.Err())
	equalLines(t, result.Stdout(), "already linked (stack #7)")
}

// Merged PRs stay in a stack on GitHub but disappear locally once yas deletes
// the branch, so they must not count as a difference.
func TestLink_IgnoresMergedPRs(t *testing.T) {
	t.Parallel()

	cli, tempDir := newLinkTester(t)

	mockStackLookup(cli, "41", stackNumber7(mergedStackPR(40, "topic-0"), openStackPR(41, "topic-a"), openStackPR(42, "topic-b")))

	setupLinkRepo(t, cli, tempDir, twoBranchStack(), "topic-b")

	result := cli.Run("link")
	assert.NilError(t, result.Err())
	equalLines(t, result.Stdout(), "already linked (stack #7)")
}

func TestLink_AddsNewPRsToExistingStack(t *testing.T) {
	t.Parallel()

	cli, tempDir := newLinkTester(t)

	mockStackLookup(cli, "41", stackNumber7(openStackPR(41, "topic-a"), openStackPR(42, "topic-b")))
	mockStackAdd(cli, "7", "43")

	setupLinkRepo(t, cli, tempDir, threeBranchStack(), "topic-c")

	result := cli.Run("link")
	assert.NilError(t, result.Err())
	equalLines(t, result.Stdout(), "added #43 to stack #7")
}

func TestLink_ReportsMismatchWithoutChangingStack(t *testing.T) {
	t.Parallel()

	cli, tempDir := newLinkTester(t)

	mockStackLookup(cli, "41", stackNumber7(openStackPR(41, "topic-a"), openStackPR(99, "other")))

	setupLinkRepo(t, cli, tempDir, twoBranchStack(), "topic-b")

	result := cli.Run("link")
	assert.NilError(t, result.Err())
	assert.Assert(t, result.StdoutContains("stack #7 contains #41, #99"), result.Stdout())
	assert.Assert(t, result.StdoutContains("yas link --unlink"), result.Stdout())
}

func TestLink_BranchWithoutPRInLineage(t *testing.T) {
	t.Parallel()

	cli, tempDir := newLinkTester(t)

	branches := threeBranchStack()
	branches["topic-b"] = yas.BranchMetadata{Name: "topic-b", Parent: "topic-a"}

	setupLinkRepo(t, cli, tempDir, branches, "topic-c")

	// No gh mocks: GitHub must not be contacted at all
	result := cli.Run("link")
	assert.NilError(t, result.Err())
	equalLines(t, result.Stdout(), "topic-b has no PR; nothing to link")
}

// sync deletes a merged branch before its children are reparented (that
// happens on the next restack), so topic-c may still name the deleted topic-b
// as its parent. The lineage must continue through topic-b to topic-a so that
// it matches the stack GitHub sees: topic-a and topic-c, with topic-b merged.
func TestLink_WalksThroughDeletedParent(t *testing.T) {
	t.Parallel()

	cli, tempDir := newLinkTester(t)

	mockStackLookup(cli, "41")
	mockStackCreate(cli, stackNumber7(openStackPR(41, "topic-a"), openStackPR(43, "topic-c")), "41", "43")

	branches := threeBranchStack()
	deletedAt := time.Now()
	topicB := branches["topic-b"]
	topicB.GitHubPullRequest.State = "MERGED"
	topicB.Deleted = &deletedAt
	branches["topic-b"] = topicB

	setupLinkRepo(t, cli, tempDir, branches, "topic-c")
	testutil.ExecOrFail(t, tempDir, "git branch -D topic-b")

	result := cli.Run("link")
	assert.NilError(t, result.Err())
	equalLines(t, result.Stdout(), "linked #41, #43 as stack #7")
}

func TestLink_SinglePRHasNothingToLink(t *testing.T) {
	t.Parallel()

	cli, tempDir := newLinkTester(t)

	setupLinkRepo(t, cli, tempDir, twoBranchStack(), "topic-a")

	result := cli.Run("link")
	assert.NilError(t, result.Err())
	equalLines(t, result.Stdout(), "nothing to link (a stack needs at least two PRs)")
}

func TestLink_StacksUnavailable(t *testing.T) {
	t.Parallel()

	cli, tempDir := newLinkTester(t)

	mockStacksUnavailable(cli)

	setupLinkRepo(t, cli, tempDir, twoBranchStack(), "topic-b")

	result := cli.Run("link")
	assert.NilError(t, result.Err())
	equalLines(t, result.Stdout(), "stacked PRs are not available for this repository")
	assert.Assert(t, !result.StderrContains("404"), "the 404 must not be surfaced as an error: %s", result.Stderr())
}

func TestLink_OtherAPIErrorsAreReported(t *testing.T) {
	t.Parallel()

	cli, tempDir := newLinkTester(t)

	mockStackLookup(cli, "41")
	mockGH(cli, append([]string{"api", "--method", "POST", stacksAPIPath}, pullRequestFields("41", "42")...)...).
		WithCode(1).
		WithStderr("gh: Validation Failed (HTTP 422)\n")

	setupLinkRepo(t, cli, tempDir, twoBranchStack(), "topic-b")

	result := cli.Run("link")
	assert.ErrorContains(t, result.Err(), "exit status 1")
	assert.Assert(t, result.StderrContains("failed to create stack: gh: Validation Failed (HTTP 422)"), result.Stderr())
}

func TestLink_DryRun(t *testing.T) {
	t.Parallel()

	cli, tempDir := newLinkTester(t)

	mockStackLookup(cli, "41")

	setupLinkRepo(t, cli, tempDir, twoBranchStack(), "topic-b")

	result := cli.Run("link", "--dry-run")
	assert.NilError(t, result.Err())
	equalLines(t, result.Stdout(), "would create stack with #41, #42")
}

func TestLink_Unlink_DissolvesStack(t *testing.T) {
	t.Parallel()

	cli, tempDir := newLinkTester(t)

	mockStackLookup(cli, "42", stackNumber7(openStackPR(41, "topic-a"), openStackPR(42, "topic-b")))
	mockStackUnstack(cli, "7", "")

	setupLinkRepo(t, cli, tempDir, twoBranchStack(), "topic-b")

	result := cli.Run("link", "--unlink")
	assert.NilError(t, result.Err())
	equalLines(t, result.Stdout(), "dissolved stack #7")
}

func TestLink_Unlink_MergedPRsRemain(t *testing.T) {
	t.Parallel()

	cli, tempDir := newLinkTester(t)

	remaining := stackNumber7(mergedStackPR(40, "topic-0"))

	mockStackLookup(cli, "42", stackNumber7(mergedStackPR(40, "topic-0"), openStackPR(41, "topic-a"), openStackPR(42, "topic-b")))
	mockStackUnstack(cli, "7", mustMarshalJSON(remaining))

	setupLinkRepo(t, cli, tempDir, twoBranchStack(), "topic-b")

	result := cli.Run("link", "--unlink")
	assert.NilError(t, result.Err())
	equalLines(t, result.Stdout(), "removed open PRs from stack #7; merged PRs remain (#40)")
}

func TestLink_Unlink_NotInStack(t *testing.T) {
	t.Parallel()

	cli, tempDir := newLinkTester(t)

	mockStackLookup(cli, "42")

	setupLinkRepo(t, cli, tempDir, twoBranchStack(), "topic-b")

	result := cli.Run("link", "--unlink")
	assert.NilError(t, result.Err())
	equalLines(t, result.Stdout(), "topic-b is not part of a stack")
}

func TestLink_Unlink_DryRun(t *testing.T) {
	t.Parallel()

	cli, tempDir := newLinkTester(t)

	mockStackLookup(cli, "42", stackNumber7(openStackPR(41, "topic-a"), openStackPR(42, "topic-b")))

	setupLinkRepo(t, cli, tempDir, twoBranchStack(), "topic-b")

	result := cli.Run("link", "--unlink", "--dry-run")
	assert.NilError(t, result.Err())
	equalLines(t, result.Stdout(), "would unstack stack #7 (#41, #42)")
}

// Submitting a stack links it on GitHub once the PRs have been annotated. Every
// submitted branch is linked in turn, but a single-PR lineage is not a stack, so
// only topic-b's lineage reaches GitHub: one lookup and one create.
func TestSubmit_LinksStackAfterAnnotate(t *testing.T) {
	t.Parallel()

	cli, tempDir := newLinkTester(t)

	fakeOrigin := t.TempDir()

	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{URL: githubPRURL("41"), BaseRefName: "main"})
	mockGitHubPRForBranch(cli, "topic-b", yas.PullRequestMetadata{URL: githubPRURL("42"), BaseRefName: "topic-a"})

	cli.Mock("gh", "pr", "view", "41", "--json", "body", "-q", ".body").WithStdout("")
	cli.Mock("gh", "pr", "view", "42", "--json", "body", "-q", ".body").WithStdout("")

	editA := cli.Mock("gh", "pr", "edit", "41", "--body", annotationBody([]string{"41", "42"}, 0))
	editB := cli.Mock("gh", "pr", "edit", "42", "--body", annotationBody([]string{"41", "42"}, 1))

	lookup := mockStackLookup(cli, "41")
	create := mockStackCreate(cli, stackNumber7(openStackPR(41, "topic-a"), openStackPR(42, "topic-b")), "41", "42")

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

		git checkout -b topic-b
		touch b
		git add b
		git commit -m "topic-b-0"
	`, map[string]string{"fakeOrigin": fakeOrigin}))

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())

	result := cli.Run("submit", "--stack")
	assert.NilError(t, result.Err())

	assert.Equal(t, lookup.CalledTimes(), 1)
	assert.Equal(t, create.CalledTimes(), 1)

	stdout := result.Stdout()
	annotateIdx := strings.Index(stdout, "Annotating PRs:")
	linkIdx := strings.Index(stdout, "Linking stacks:")
	assert.Assert(t, annotateIdx >= 0 && linkIdx > annotateIdx, stdout)
	assert.Assert(t, strings.Contains(stdout, "topic-b: linked #41, #42 as stack #7"), stdout)

	// Linking happens after every annotation has finished
	createdAt := create.Calls()[0].Timestamp
	for _, edit := range []*gocmdtester.Mock{editA, editB} {
		assert.Assert(t, !createdAt.Before(edit.Calls()[0].Timestamp), "stack created before PRs were annotated")
	}
}

// When stacked PRs are not enabled, submit says so once and still succeeds.
func TestSubmit_SkipsLinkingWhenStacksUnavailable(t *testing.T) {
	t.Parallel()

	cli, tempDir := newLinkTester(t)

	fakeOrigin := t.TempDir()

	mockGitHubPRForBranch(cli, "topic-a", yas.PullRequestMetadata{URL: githubPRURL("41"), BaseRefName: "main"})
	mockGitHubPRForBranch(cli, "topic-b", yas.PullRequestMetadata{URL: githubPRURL("42"), BaseRefName: "topic-a"})

	cli.Mock("gh", "pr", "view", "41", "--json", "body", "-q", ".body").WithStdout("")
	cli.Mock("gh", "pr", "view", "42", "--json", "body", "-q", ".body").WithStdout("")
	cli.Mock("gh", "pr", "edit", "41", "--body", annotationBody([]string{"41", "42"}, 0))
	cli.Mock("gh", "pr", "edit", "42", "--body", annotationBody([]string{"41", "42"}, 1))

	unavailable := mockStacksUnavailable(cli)

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

		git checkout -b topic-b
		touch b
		git add b
		git commit -m "topic-b-0"
	`, map[string]string{"fakeOrigin": fakeOrigin}))

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())

	result := cli.Run("submit", "--stack")
	assert.NilError(t, result.Err())

	assert.Equal(t, unavailable.CalledTimes(), 1)
	assert.Assert(t, result.StdoutContains("stacked PRs are not available for this repository; skipping stack linking"), result.Stdout())
	assert.Assert(t, result.StdoutContains("Successfully submitted and annotated 2 branch(es)"), result.Stdout())
}
