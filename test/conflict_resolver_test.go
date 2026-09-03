package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"gotest.tools/v3/assert"
)

// fakeClaudeScript stands in for the `claude` CLI. It records every
// invocation (arguments separated by a sentinel line) to $FAKE_CLAUDE_LOG and
// then acts according to $FAKE_CLAUDE_MODE:
//
//   - resolve (default): strip the (7-character) conflict markers from every
//     unmerged file, keeping both sides' content in order (and dropping the
//     base section that diff3/zdiff3 conflict styles add)
//   - extra: like resolve, but also create an unrelated helper.txt
//   - clobber: like resolve, but also append to notes.txt and secret.env,
//     which the test has left untracked and ignored respectively
//   - checkout-sub: check out origin/side in the submodule at ./sub, the way
//     a tool would resolve a gitlink conflict
//   - noop: exit successfully without touching anything
//   - fail: exit non-zero
//   - fail-after-edit: create an unrelated helper.txt, then exit non-zero
const fakeClaudeScript = `#!/bin/sh
{
  for arg in "$@"; do
    printf '%s\n' "$arg"
    echo '---ARG---'
  done
  echo '---CALL---'
} >> "$FAKE_CLAUDE_LOG"

case "${FAKE_CLAUDE_MODE:-resolve}" in
  resolve|extra|clobber)
    for f in $(git diff --name-only --diff-filter=U); do
      awk '
        /^<<<<<<< / { next }
        /^\|\|\|\|\|\|\| / { base = 1; next }
        /^=======$/ { base = 0; next }
        /^>>>>>>> / { next }
        !base
      ' "$f" > "$f.resolved"
      mv "$f.resolved" "$f"
    done
    if [ "$FAKE_CLAUDE_MODE" = "extra" ]; then
      echo "helper" > helper.txt
    fi
    if [ "$FAKE_CLAUDE_MODE" = "clobber" ]; then
      echo "clobbered" >> notes.txt
      echo "clobbered" >> secret.env
    fi
    echo "fake claude: resolved conflicts"
    ;;
  checkout-sub)
    git -C sub checkout -q origin/side
    echo "fake claude: checked out submodule commit"
    ;;
  noop)
    echo "fake claude: did nothing"
    ;;
  fail)
    echo "fake claude: failing on purpose" >&2
    exit 2
    ;;
  fail-after-edit)
    echo "helper" > helper.txt
    echo "fake claude: failing after a partial edit" >&2
    exit 2
    ;;
esac
`

// setupFakeClaude installs the fake claude script in a directory and returns
// the CLI options needed to make yas use it, plus the path of the invocation
// log.
func setupFakeClaude(t *testing.T, tempDir string, mode string) (opts []gocmdtester.Option, logPath string) {
	t.Helper()

	// Keep the script and its log outside the repository under test: yas
	// reports anything the resolver leaves behind in the working tree.
	supportDir := t.TempDir()

	binDir := filepath.Join(supportDir, "fake-bin")
	assert.NilError(t, os.MkdirAll(binDir, 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(binDir, "claude"), []byte(fakeClaudeScript), 0o755))

	logPath = filepath.Join(supportDir, "fake-claude.log")

	return []gocmdtester.Option{
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("PATH", binDir+":"+os.Getenv("PATH")),
		gocmdtester.WithEnv("FAKE_CLAUDE_LOG", logPath),
		gocmdtester.WithEnv("FAKE_CLAUDE_MODE", mode),
	}, logPath
}

// fakeClaudeCalls returns the argument lists of every recorded invocation.
func fakeClaudeCalls(t *testing.T, logPath string) [][]string {
	t.Helper()

	b, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil
	}

	assert.NilError(t, err)

	var calls [][]string

	for _, call := range strings.Split(string(b), "---CALL---\n") {
		if strings.TrimSpace(call) == "" {
			continue
		}

		var args []string

		for _, arg := range strings.Split(call, "---ARG---\n") {
			if arg == "" {
				continue
			}

			args = append(args, strings.TrimSuffix(arg, "\n"))
		}

		calls = append(calls, args)
	}

	return calls
}

// setupSingleConflictRepo creates: main -> topic-a -> topic-b, where main has
// moved on with a change to file.txt that conflicts with topic-a's change.
// Rebasing topic-b afterwards is clean. The repo is left on topic-b.
func setupSingleConflictRepo(t *testing.T, tempDir string) {
	t.Helper()

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		echo "line1" > file.txt
		git add file.txt
		git commit -m "main-0"

		git checkout -b topic-a
		echo "line2-from-a" >> file.txt
		git add file.txt
		git commit -m "topic-a-0"

		git checkout -b topic-b
		echo "line3-from-b" >> file.txt
		git add file.txt
		git commit -m "topic-b-0"

		git checkout main
		echo "line2-from-main" >> file.txt
		git add file.txt
		git commit -m "main-1"

		git checkout topic-b
	`)
}

func trackSingleConflictRepo(t *testing.T, cli *gocmdtester.CmdTester) {
	t.Helper()

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())
	assert.NilError(t, cli.Run("add", "topic-b", "--parent=topic-a").Err())
}

func fileOnBranch(t *testing.T, tempDir, branch, file string) string {
	t.Helper()

	return mustExecOutput(tempDir, "git", "show", branch+":"+file)
}

func TestConflictResolver_ClaudeContinue(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	opts, logPath := setupFakeClaude(t, tempDir, "resolve")
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go", opts...)

	setupSingleConflictRepo(t, tempDir)
	trackSingleConflictRepo(t, cli)

	result := cli.Run("restack", "--all", "--conflict-resolver=claude", "--after-resolve=continue")
	assert.Equal(t, result.ExitCode(), 0, "restack should complete: %s", result.Stderr())
	assert.Assert(t, result.StdoutContains("resolving with claude"), "stdout: %s", result.Stdout())

	// The whole restack finished: no state left behind, back on the starting branch.
	assert.Assert(t, !assertRestackStateExists(t, tempDir))
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--show-current"), "topic-b")

	// The resolver's content was committed and the rest of the stack followed.
	equalLines(t, fileOnBranch(t, tempDir, "topic-a", "file.txt"), "line1\nline2-from-main\nline2-from-a")
	equalLines(t, fileOnBranch(t, tempDir, "topic-b", "file.txt"), "line1\nline2-from-main\nline2-from-a\nline3-from-b")
	equalLines(t, mustExecOutput(tempDir, "git", "log", "--pretty=%s", "topic-b"), "topic-b-0\ntopic-a-0\nmain-1\nmain-0")

	// Claude was invoked exactly once, non-interactively, with a prompt
	// describing the conflict and without permission to touch git state.
	calls := fakeClaudeCalls(t, logPath)
	assert.Equal(t, len(calls), 1, "claude should be called once, got %v", calls)

	args := calls[0]
	assert.Equal(t, args[0], "-p")

	prompt := args[1]
	for _, want := range []string{"`topic-a`", "`main`", "- file.txt", `"topic-a-0"`} {
		assert.Assert(t, strings.Contains(prompt, want), "prompt should mention %q:\n%s", want, prompt)
	}

	assert.DeepEqual(t, args[2:4], []string{"--permission-mode", "acceptEdits"})
	assert.Equal(t, args[4], "--allowedTools")
	assert.Assert(t, !strings.Contains(args[5], "git add"))
}

func TestConflictResolver_ClaudeStopIsDefault(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	opts, logPath := setupFakeClaude(t, tempDir, "resolve")
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go", opts...)

	setupSingleConflictRepo(t, tempDir)
	trackSingleConflictRepo(t, cli)

	result := cli.Run("restack", "--all", "--conflict-resolver=claude")
	assert.Equal(t, result.ExitCode(), 1, "restack should stop for review")
	assert.Assert(t, result.StderrContains("resolved by claude"), "stderr: %s", result.Stderr())
	assert.Assert(t, result.StderrContains("yas continue"), "stderr: %s", result.Stderr())
	assert.Equal(t, len(fakeClaudeCalls(t, logPath)), 1)

	// Paused mid-rebase with the resolution applied but left unstaged for review.
	assert.Assert(t, assertRestackStateExists(t, tempDir))

	state, err := yas.LoadRestackState(tempDir)
	assert.NilError(t, err)
	assert.Equal(t, state.CurrentBranch, "topic-a")
	assert.Equal(t, state.ConflictResolver, "claude")
	assert.Equal(t, state.AfterResolve, "stop")

	equalLines(t, mustExecOutput(tempDir, "git", "diff", "--name-only", "--diff-filter=U"), "file.txt")

	content, err := os.ReadFile(filepath.Join(tempDir, "file.txt"))
	assert.NilError(t, err)
	equalLines(t, string(content), "line1\nline2-from-main\nline2-from-a")

	// The user reviews, stages and continues as with a manual resolution.
	testutil.ExecOrFail(t, tempDir, "git add file.txt")
	assert.NilError(t, cli.Run("continue").Err())

	assert.Assert(t, !assertRestackStateExists(t, tempDir))
	equalLines(t, fileOnBranch(t, tempDir, "topic-b", "file.txt"), "line1\nline2-from-main\nline2-from-a\nline3-from-b")
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--show-current"), "topic-b")

	// topic-b rebased cleanly, so claude was not needed again.
	assert.Equal(t, len(fakeClaudeCalls(t, logPath)), 1)
}

func TestConflictResolver_MarkersRemainStopsUnlessForced(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	opts, logPath := setupFakeClaude(t, tempDir, "noop")
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go", opts...)

	setupSingleConflictRepo(t, tempDir)
	trackSingleConflictRepo(t, cli)

	// The resolver runs but leaves the markers in place: yas must not continue.
	result := cli.Run("restack", "--all", "--conflict-resolver=claude", "--after-resolve=continue")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("left conflict markers in:"), "stderr: %s", result.Stderr())
	assert.Assert(t, result.StderrContains("file.txt"), "stderr: %s", result.Stderr())
	assert.Assert(t, assertRestackStateExists(t, tempDir))
	assert.Equal(t, len(fakeClaudeCalls(t, logPath)), 1)

	// Nothing was staged or committed.
	equalLines(t, mustExecOutput(tempDir, "git", "diff", "--name-only", "--diff-filter=U"), "file.txt")

	// Back out and try again with force: markers get committed and the
	// restack runs to completion.
	assert.NilError(t, cli.Run("abort").Err())

	result = cli.Run("restack", "--all", "--conflict-resolver=claude", "--after-resolve=force")
	assert.Equal(t, result.ExitCode(), 0, "forced restack should complete: %s", result.Stderr())
	assert.Assert(t, result.StdoutContains("conflict markers remain in file.txt; continuing anyway"), "stdout: %s", result.Stdout())
	assert.Assert(t, !assertRestackStateExists(t, tempDir))

	committed := fileOnBranch(t, tempDir, "topic-a", "file.txt")
	assert.Assert(t, strings.Contains(committed, "<<<<<<<"), "markers should have been committed: %s", committed)
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--show-current"), "topic-b")
}

func TestConflictResolver_ResolverFailure(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	opts, logPath := setupFakeClaude(t, tempDir, "fail")
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go", opts...)

	setupSingleConflictRepo(t, tempDir)
	trackSingleConflictRepo(t, cli)

	result := cli.Run("restack", "--all", "--conflict-resolver=claude", "--after-resolve=continue")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("conflict resolver claude failed"), "stderr: %s", result.Stderr())
	assert.Assert(t, result.StderrContains("yas continue"), "stderr: %s", result.Stderr())
	assert.Equal(t, len(fakeClaudeCalls(t, logPath)), 1)

	// The rebase is still paused so the user can take over.
	assert.Assert(t, assertRestackStateExists(t, tempDir))
	equalLines(t, mustExecOutput(tempDir, "git", "diff", "--name-only", "--diff-filter=U"), "file.txt")

	assert.NilError(t, cli.Run("abort").Err())
}

func TestConflictResolver_FailureStillReportsStrayEdits(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	opts, logPath := setupFakeClaude(t, tempDir, "fail-after-edit")
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go", opts...)

	setupSingleConflictRepo(t, tempDir)
	trackSingleConflictRepo(t, cli)

	// The tool creates helper.txt and then dies. The failure is reported,
	// and so is the file it left behind outside the conflict.
	result := cli.Run("restack", "--all", "--conflict-resolver=claude", "--after-resolve=continue")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("conflict resolver claude failed"), "stderr: %s", result.Stderr())
	assert.Assert(t, result.StderrContains("changed files outside the conflicted paths"), "stderr: %s", result.Stderr())
	assert.Assert(t, result.StderrContains("- helper.txt"), "stderr: %s", result.Stderr())
	assert.Assert(t, result.StderrContains("yas continue"), "stderr: %s", result.Stderr())
	assert.Equal(t, len(fakeClaudeCalls(t, logPath)), 1)

	assert.Assert(t, assertRestackStateExists(t, tempDir))
	equalLines(t, mustExecOutput(tempDir, "git", "diff", "--name-only", "--diff-filter=U"), "file.txt")
	assert.Assert(t, strings.Contains(mustExecOutput(tempDir, "git", "status", "--porcelain"), "?? helper.txt"), "helper.txt should be left for the user to inspect")
}

func TestConflictResolver_ConfigAndFlagOverride(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	opts, logPath := setupFakeClaude(t, tempDir, "resolve")
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go", opts...)

	setupSingleConflictRepo(t, tempDir)
	trackSingleConflictRepo(t, cli)

	// Invalid values are rejected and not written.
	result := cli.Run("config", "set", "--conflict-resolver=bogus")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains(`invalid conflict-resolver "bogus"`), "stderr: %s", result.Stderr())

	result = cli.Run("config", "set", "--after-resolve=maybe")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains(`invalid after-resolve "maybe"`), "stderr: %s", result.Stderr())

	// Defaults are none/stop.
	result = cli.Run("config", "show")
	assert.NilError(t, result.Err())
	assert.Assert(t, result.StdoutContains(`ConflictResolver: (string) (len=4) "none"`), "stdout: %s", result.Stdout())
	assert.Assert(t, result.StdoutContains(`AfterResolve: (string) (len=4) "stop"`), "stdout: %s", result.Stdout())

	assert.NilError(t, cli.Run("config", "set", "--conflict-resolver=claude", "--after-resolve=continue").Err())

	result = cli.Run("config", "show")
	assert.NilError(t, result.Err())
	assert.Assert(t, result.StdoutContains(`ConflictResolver: (string) (len=6) "claude"`), "stdout: %s", result.Stdout())
	assert.Assert(t, result.StdoutContains(`AfterResolve: (string) (len=8) "continue"`), "stdout: %s", result.Stdout())

	// A flag overrides config: with none, the conflict stops the restack and
	// claude is never invoked.
	result = cli.Run("restack", "--all", "--conflict-resolver=none")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("Fix conflicts and run 'yas continue'"), "stderr: %s", result.Stderr())
	assert.Equal(t, len(fakeClaudeCalls(t, logPath)), 0)
	assert.NilError(t, cli.Run("abort").Err())

	// Invalid flag values are rejected before anything is touched.
	result = cli.Run("restack", "--all", "--conflict-resolver=bogus")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains(`invalid conflict-resolver "bogus"`), "stderr: %s", result.Stderr())
	assert.Assert(t, !assertRestackStateExists(t, tempDir))

	// With no flags the config applies: resolved and continued automatically.
	result = cli.Run("restack", "--all")
	assert.Equal(t, result.ExitCode(), 0, "restack should complete from config: %s", result.Stderr())
	assert.Equal(t, len(fakeClaudeCalls(t, logPath)), 1)
	assert.Assert(t, !assertRestackStateExists(t, tempDir))
	equalLines(t, fileOnBranch(t, tempDir, "topic-a", "file.txt"), "line1\nline2-from-main\nline2-from-a")
}

func TestConflictResolver_ContinueInheritsSettings(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	opts, logPath := setupFakeClaude(t, tempDir, "resolve")
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go", opts...)

	// Two conflicts in the stack: topic-a conflicts with main on file.txt, and
	// topic-b conflicts with main on other.txt (surfacing when topic-b is
	// rebased onto the rebased topic-a).
	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		echo "line1" > file.txt
		echo "o1" > other.txt
		git add file.txt other.txt
		git commit -m "main-0"

		git checkout -b topic-a
		echo "a" >> file.txt
		git add file.txt
		git commit -m "topic-a-0"

		git checkout -b topic-b
		echo "b" >> other.txt
		git add other.txt
		git commit -m "topic-b-0"

		git checkout main
		echo "main" >> file.txt
		echo "main" >> other.txt
		git add file.txt other.txt
		git commit -m "main-1"

		git checkout topic-b
	`)
	trackSingleConflictRepo(t, cli)

	// Start with claude and the default (stop) behaviour.
	result := cli.Run("restack", "--all", "--conflict-resolver=claude")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("conflicts in file.txt were resolved by claude"), "stderr: %s", result.Stderr())
	assert.Equal(t, len(fakeClaudeCalls(t, logPath)), 1)

	// Plain `yas continue` picks the resolver up from the saved state and
	// handles the second conflict the same way.
	testutil.ExecOrFail(t, tempDir, "git add file.txt")

	result = cli.Run("continue")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("conflicts in other.txt were resolved by claude"), "stderr: %s", result.Stderr())
	assert.Equal(t, len(fakeClaudeCalls(t, logPath)), 2)

	state, err := yas.LoadRestackState(tempDir)
	assert.NilError(t, err)
	assert.Equal(t, state.CurrentBranch, "topic-b")
	assert.Equal(t, state.ConflictResolver, "claude")

	// Recreate the conflict so the resolver has to run again, this time from
	// within `yas continue` and with a flag overriding the saved behaviour.
	testutil.ExecOrFail(t, tempDir, "git checkout -m -- other.txt")

	result = cli.Run("continue", "--after-resolve=continue")
	assert.Equal(t, result.ExitCode(), 0, "continue should finish the restack: %s", result.Stderr())
	assert.Equal(t, len(fakeClaudeCalls(t, logPath)), 3)
	assert.Assert(t, !assertRestackStateExists(t, tempDir))

	equalLines(t, fileOnBranch(t, tempDir, "topic-a", "file.txt"), "line1\nmain\na")
	equalLines(t, fileOnBranch(t, tempDir, "topic-b", "other.txt"), "o1\nmain\nb")
	equalLines(t, mustExecOutput(tempDir, "git", "branch", "--show-current"), "topic-b")
}

// gitOnlyPath returns a PATH containing git (and the shell utilities yas
// itself needs) but definitely no `claude`.
func gitOnlyPath(t *testing.T, tempDir string) string {
	t.Helper()

	binDir := filepath.Join(tempDir, "only-git")
	assert.NilError(t, os.MkdirAll(binDir, 0o755))

	for _, tool := range []string{"git", "test", "sh", "cat", "true"} {
		// `which` (not `command -v`) so shell builtins like test resolve to
		// the real binary rather than the bare name.
		toolPath := strings.TrimSpace(mustExecOutput(tempDir, "which", tool))
		assert.Assert(t, filepath.IsAbs(toolPath), "could not locate %s: %q", tool, toolPath)
		assert.NilError(t, os.Symlink(toolPath, filepath.Join(binDir, tool)))
	}

	return binDir
}

func TestConflictResolver_MissingTool(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("PATH", gitOnlyPath(t, tempDir)),
	)

	setupSingleConflictRepo(t, tempDir)
	trackSingleConflictRepo(t, cli)

	result := cli.Run("restack", "--all", "--conflict-resolver=claude")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains(`requires the "claude" command`), "stderr: %s", result.Stderr())

	// Failed before any rebase started.
	assert.Assert(t, !assertRestackStateExists(t, tempDir))
	equalLines(t, mustExecOutput(tempDir, "git", "log", "--pretty=%s", "topic-a"), "topic-a-0\nmain-0")
}

func TestConflictResolver_ModifyDeleteRequiresVerification(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	// "resolve" mode strips markers, but a modify/delete conflict has none, so
	// the fake leaves the file byte-for-byte as git did.
	opts, logPath := setupFakeClaude(t, tempDir, "resolve")
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go", opts...)

	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		echo "keep" > file.txt
		echo "doomed" > doomed.txt
		git add file.txt doomed.txt
		git commit -m "main-0"

		git checkout -b topic-a
		echo "edited on topic-a" > doomed.txt
		git add doomed.txt
		git commit -m "topic-a-0"

		git checkout main
		git rm -q doomed.txt
		git commit -m "main-1"

		git checkout topic-a
	`)

	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// No markers to check, and the resolver changed nothing: yas must not
	// silently stage git's version and carry on.
	result := cli.Run("restack", "--conflict-resolver=claude", "--after-resolve=continue")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("cannot be verified"), "stderr: %s", result.Stderr())
	assert.Assert(t, result.StderrContains("- doomed.txt"), "stderr: %s", result.Stderr())
	assert.Assert(t, assertRestackStateExists(t, tempDir))
	assert.Equal(t, len(fakeClaudeCalls(t, logPath)), 1)
	equalLines(t, mustExecOutput(tempDir, "git", "diff", "--name-only", "--diff-filter=U"), "doomed.txt")

	// force accepts the file as-is (keeping topic-a's edit) and completes.
	assert.NilError(t, cli.Run("abort").Err())

	result = cli.Run("restack", "--conflict-resolver=claude", "--after-resolve=force")
	assert.Equal(t, result.ExitCode(), 0, "forced restack should complete: %s", result.Stderr())
	assert.Assert(t, result.StdoutContains("unable to verify resolution of doomed.txt; continuing anyway"), "stdout: %s", result.Stdout())
	assert.Assert(t, !assertRestackStateExists(t, tempDir))
	equalLines(t, fileOnBranch(t, tempDir, "topic-a", "doomed.txt"), "edited on topic-a")
	equalLines(t, mustExecOutput(tempDir, "git", "log", "--pretty=%s", "topic-a"), "topic-a-0\nmain-1\nmain-0")
}

func TestConflictResolver_ContinueDoesNotRequireToolAfterManualFix(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	opts, logPath := setupFakeClaude(t, tempDir, "resolve")
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go", opts...)

	setupSingleConflictRepo(t, tempDir)
	trackSingleConflictRepo(t, cli)

	// Start the restack with claude; it resolves and stops for review.
	result := cli.Run("restack", "--all", "--conflict-resolver=claude")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("resolved by claude"), "stderr: %s", result.Stderr())
	assert.Equal(t, len(fakeClaudeCalls(t, logPath)), 1)

	// The user stages the resolution and continues from a shell where claude
	// isn't on PATH. Nothing else conflicts, so the saved resolver setting
	// must not block the recovery.
	testutil.ExecOrFail(t, tempDir, "git add file.txt")

	noTool := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("PATH", gitOnlyPath(t, tempDir)),
	)

	result = noTool.Run("continue")
	assert.Equal(t, result.ExitCode(), 0, "continue should succeed without the tool: %s", result.Stderr())
	assert.Assert(t, !assertRestackStateExists(t, tempDir))
	equalLines(t, fileOnBranch(t, tempDir, "topic-b", "file.txt"), "line1\nline2-from-main\nline2-from-a\nline3-from-b")
	assert.Equal(t, len(fakeClaudeCalls(t, logPath)), 1)
}

func TestConflictResolver_ContinueReportsMissingToolOnlyWhenNeeded(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	opts, _ := setupFakeClaude(t, tempDir, "resolve")
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go", opts...)

	setupSingleConflictRepo(t, tempDir)
	trackSingleConflictRepo(t, cli)

	result := cli.Run("restack", "--all", "--conflict-resolver=claude")
	assert.Equal(t, result.ExitCode(), 1)

	// Recreate the conflict so continuing genuinely needs the resolver.
	testutil.ExecOrFail(t, tempDir, "git checkout -m -- file.txt")

	noTool := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("PATH", gitOnlyPath(t, tempDir)),
	)

	result = noTool.Run("continue")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains(`requires the "claude" command`), "stderr: %s", result.Stderr())
	assert.Assert(t, result.StderrContains("yas continue"), "stderr: %s", result.Stderr())
	assert.Assert(t, assertRestackStateExists(t, tempDir), "state must survive so the user can recover")
}

func TestConflictResolver_ChangesOutsideConflictedPaths(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	opts, logPath := setupFakeClaude(t, tempDir, "extra")
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go", opts...)

	setupSingleConflictRepo(t, tempDir)
	trackSingleConflictRepo(t, cli)

	// The resolver fixes file.txt but also creates helper.txt. Staging only
	// file.txt would leave helper.txt out of the commit, so yas stops.
	result := cli.Run("restack", "--all", "--conflict-resolver=claude", "--after-resolve=continue")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("changed files outside the conflicted paths"), "stderr: %s", result.Stderr())
	assert.Assert(t, result.StderrContains("- helper.txt"), "stderr: %s", result.Stderr())
	assert.Assert(t, assertRestackStateExists(t, tempDir))
	assert.Equal(t, len(fakeClaudeCalls(t, logPath)), 1)

	// Nothing staged; the user decides what belongs.
	equalLines(t, mustExecOutput(tempDir, "git", "diff", "--name-only", "--diff-filter=U"), "file.txt")

	// With force, the restack completes and helper.txt is left untracked.
	assert.NilError(t, cli.Run("abort").Err())
	testutil.ExecOrFail(t, tempDir, "rm helper.txt")

	result = cli.Run("restack", "--all", "--conflict-resolver=claude", "--after-resolve=force")
	assert.Equal(t, result.ExitCode(), 0, "forced restack should complete: %s", result.Stderr())
	assert.Assert(t, result.StdoutContains("changed files outside the conflicted paths (helper.txt); they are left unstaged"), "stdout: %s", result.Stdout())
	assert.Assert(t, !assertRestackStateExists(t, tempDir))
	equalLines(t, fileOnBranch(t, tempDir, "topic-a", "file.txt"), "line1\nline2-from-main\nline2-from-a")
	assert.Assert(t, strings.Contains(mustExecOutput(tempDir, "git", "status", "--porcelain"), "?? helper.txt"), "helper.txt should be left untracked")
}

func TestConflictResolver_OverwritingDirtyFilesStops(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	opts, _ := setupFakeClaude(t, tempDir, "clobber")
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go", opts...)

	setupSingleConflictRepo(t, tempDir)
	trackSingleConflictRepo(t, cli)

	// An untracked file and an ignored file exist before the resolver runs.
	// Their status codes ("??" and "!!") don't change when they're overwritten,
	// so yas must compare their contents, not just their status.
	testutil.ExecOrFail(t, tempDir, `
		echo "my notes" > notes.txt
		echo "secret.env" >> .git/info/exclude
		echo "shh" > secret.env
	`)

	result := cli.Run("restack", "--all", "--conflict-resolver=claude", "--after-resolve=continue")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("changed files outside the conflicted paths"), "stderr: %s", result.Stderr())
	assert.Assert(t, result.StderrContains("- notes.txt"), "stderr: %s", result.Stderr())
	assert.Assert(t, result.StderrContains("- secret.env"), "stderr: %s", result.Stderr())
	assert.Assert(t, assertRestackStateExists(t, tempDir))

	// Nothing staged; the user decides what belongs.
	equalLines(t, mustExecOutput(tempDir, "git", "diff", "--name-only", "--diff-filter=U"), "file.txt")
}

func TestConflictResolver_CustomMarkerSize(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	opts, logPath := setupFakeClaude(t, tempDir, "resolve")
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go", opts...)

	// Like setupSingleConflictRepo, but file.txt uses 12-character markers.
	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main

		echo "file.txt conflict-marker-size=12" > .gitattributes
		echo "line1" > file.txt
		git add -A
		git commit -m "main-0"

		git checkout -b topic-a
		echo "line2-from-a" >> file.txt
		git commit -am "topic-a-0"

		git checkout main
		echo "line2-from-main" >> file.txt
		git commit -am "main-1"

		git checkout topic-a
	`)
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// The fake resolver only knows how to strip 7-character markers, so it
	// leaves the 12-character ones behind. yas must recognise them as
	// markers without being told the size.
	result := cli.Run("restack", "--all", "--conflict-resolver=claude", "--after-resolve=continue")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("left conflict markers in:"), "stderr: %s", result.Stderr())
	assert.Assert(t, result.StderrContains("- file.txt"), "stderr: %s", result.Stderr())
	assert.Assert(t, assertRestackStateExists(t, tempDir))
	assert.Equal(t, len(fakeClaudeCalls(t, logPath)), 1)

	content, err := os.ReadFile(filepath.Join(tempDir, "file.txt"))
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(string(content), "<<<<<<<<<<<< "), "file.txt should hold 12-character markers: %s", content)

	// Nothing was staged or committed with markers in it.
	equalLines(t, mustExecOutput(tempDir, "git", "diff", "--name-only", "--diff-filter=U"), "file.txt")
	equalLines(t, fileOnBranch(t, tempDir, "topic-a", "file.txt"), "line1\nline2-from-a")
}

func TestConflictResolver_SubmoduleConflict(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	opts, logPath := setupFakeClaude(t, tempDir, "noop")
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go", opts...)

	// A submodule whose two branches diverge.
	subDir := t.TempDir()
	testutil.ExecOrFail(t, subDir, `
		git init --initial-branch=main
		echo "s0" > s.txt
		git add s.txt
		git commit -m "s0"
		git checkout -b side
		echo "s2" > s.txt
		git commit -am "s2"
		git checkout main
		echo "s1" > s.txt
		git commit -am "s1"
	`)

	// main and topic-a each point the submodule at a different commit.
	testutil.ExecOrFail(t, tempDir, `
		git init --initial-branch=main
		echo "line1" > file.txt
		git add file.txt
		git commit -m "main-0"
		git -c protocol.file.allow=always submodule add -q "`+subDir+`" sub
		git commit -qm "add submodule"

		git checkout -b topic-a
		git -C sub checkout -q origin/side
		git add sub
		git commit -m "topic-a-0"

		git checkout main
		git -C sub checkout -q origin/main~1
		git add sub
		git commit -m "main-1"

		git checkout topic-a
	`)
	assert.NilError(t, cli.Run("config", "set", "--trunk-branch=main").Err())
	assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

	// The conflicted path is a directory. The resolver is still invoked, and
	// since a gitlink has no markers and the tool left it alone, yas stops
	// for manual resolution rather than staging one side.
	result := cli.Run("restack", "--all", "--conflict-resolver=claude", "--after-resolve=continue")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains("cannot be verified"), "stderr: %s", result.Stderr())
	assert.Assert(t, result.StderrContains("- sub"), "stderr: %s", result.Stderr())
	assert.Assert(t, assertRestackStateExists(t, tempDir))
	assert.Equal(t, len(fakeClaudeCalls(t, logPath)), 1)
	equalLines(t, mustExecOutput(tempDir, "git", "diff", "--name-only", "--diff-filter=U"), "sub")

	// A tool that checks out a commit inside the submodule has resolved the
	// gitlink: yas sees the new commit, stages it and finishes the rebase.
	assert.NilError(t, cli.Run("abort").Err())

	sideCommit := strings.TrimSpace(mustExecOutput(filepath.Join(tempDir, "sub"), "git", "rev-parse", "origin/side"))
	checkoutOpts, _ := setupFakeClaude(t, tempDir, "checkout-sub")
	resolving := gocmdtester.FromPath(t, "../cmd/yas/main.go", checkoutOpts...)

	result = resolving.Run("restack", "--all", "--conflict-resolver=claude", "--after-resolve=continue")
	assert.Equal(t, result.ExitCode(), 0, "stderr: %s", result.Stderr())
	assert.Assert(t, !assertRestackStateExists(t, tempDir))
	assert.Assert(t, strings.Contains(mustExecOutput(tempDir, "git", "ls-tree", "topic-a", "sub"), sideCommit), "topic-a should record the checked-out submodule commit")
}

func TestConflictResolver_DryRunDoesNotNeedTool(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	setupSingleConflictRepo(t, tempDir)

	noTool := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("PATH", gitOnlyPath(t, tempDir)),
	)
	trackSingleConflictRepo(t, noTool)

	// A dry run never launches the resolver, so its absence must not stop
	// the plan from being shown...
	result := noTool.Run("restack", "--all", "--dry-run", "--conflict-resolver=claude")
	assert.Equal(t, result.ExitCode(), 0, "stderr: %s", result.Stderr())
	assert.Assert(t, result.StdoutContains("Would restack"), "stdout: %s", result.Stdout())
	assert.Assert(t, !assertRestackStateExists(t, tempDir))

	// ...but the settings themselves are still validated.
	result = noTool.Run("restack", "--all", "--dry-run", "--conflict-resolver=bogus")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains(`invalid conflict-resolver "bogus"`), "stderr: %s", result.Stderr())

	// A real run still reports the missing tool before touching anything.
	result = noTool.Run("restack", "--all", "--conflict-resolver=claude")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains(`requires the "claude" command`), "stderr: %s", result.Stderr())
	assert.Assert(t, !assertRestackStateExists(t, tempDir))
}

func TestConflictResolver_SyncValidatesBeforeDoingAnything(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	opts, _ := setupFakeClaude(t, tempDir, "resolve")
	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go", opts...)

	setupSingleConflictRepo(t, tempDir)
	trackSingleConflictRepo(t, cli)

	result := cli.Run("sync", "--restack", "--conflict-resolver=bogus")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains(`invalid conflict-resolver "bogus"`), "stderr: %s", result.Stderr())
	assert.Assert(t, !result.StdoutContains("Pulling"), "sync must fail before pulling trunk: %s", result.Stdout())
	assert.Assert(t, !result.StdoutContains("Checking for merged PRs"), "sync must fail before touching branches: %s", result.Stdout())

	// Same for a resolver whose binary is missing.
	noTool := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
		gocmdtester.WithEnv("PATH", gitOnlyPath(t, tempDir)),
	)

	result = noTool.Run("sync", "--restack", "--conflict-resolver=claude")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, result.StderrContains(`requires the "claude" command`), "stderr: %s", result.Stderr())
	assert.Assert(t, !result.StdoutContains("Pulling"), "sync must fail before pulling trunk: %s", result.Stdout())
}
