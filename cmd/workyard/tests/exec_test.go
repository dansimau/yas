package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestExec_RunsInEveryRepository(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	// The command runs with the repository as its working directory.
	result := newCLI(t, filepath.Join(target, "sub")).Run("exec", "pwd")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, strings.Count(result.Stdout(), "==>"), 4)

	realTarget, err := filepath.EvalSymlinks(target)
	assert.NilError(t, err)

	for _, repo := range fixtureRepos {
		assert.Assert(t, cmp.Contains(result.Stdout(), "\n"+filepath.Join(realTarget, repo)+"\n"), repo)
	}

	// Everything after the first non-option goes to the command untouched.
	result = newCLI(t, target).Run("exec", "git", "log", "--oneline", "-1")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, strings.Count(result.Stdout(), " initial"), 4)

	// Options workyard does not know are passed through too, even before the
	// first positional argument: here "--no-pager" becomes the program to run.
	result = newCLI(t, target).Run("exec", "--no-pager", "git", "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, cmp.Contains(result.Stderr(), "--no-pager failed in 4 of 4 repositories"))

	result = newCLI(t, target).Run("exec", "git", "--no-pager", "rev-parse", "--abbrev-ref", "HEAD")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, strings.Count(result.Stdout(), "\nfeature\n"), 3)
	assert.Assert(t, cmp.Contains(result.Stdout(), "\nHEAD\n"))

	// "--" passes anything, including options workyard would otherwise take.
	result = newCLI(t, target).Run("exec", "--", "git", "--version")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, strings.Count(result.Stdout(), "git version"), 4)
}

func TestExec_ShellCommands(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	result := newCLI(t, target).Run("exec", "sh", "-c", "echo in $(basename \"$PWD\")")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())

	for _, repo := range fixtureRepos {
		assert.Assert(t, cmp.Contains(result.Stdout(), "\nin "+filepath.Base(repo)+"\n"), repo)
	}
}

func TestExec_SerialIsTheDefault(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	// Repositories run one after another in path order, each preceded by a
	// header, which is printed even when the command produces no output.
	// Headers after the first are separated from the previous output by a
	// blank line.
	result := newCLI(t, target).Run("exec", "true")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, result.Stdout(), strings.Join([]string{
		"==> repoA (feature)",
		"",
		"==> sub/deep/repoB (feature)",
		"",
		"==> wt/main (feature)",
		"",
		"==> wt/other (HEAD)",
		"",
	}, "\n"))

	// Output is interleaved with the headers as it happens.
	result = newCLI(t, target).Run("exec", "sh", "-c", "basename \"$PWD\"")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Assert(t, cmp.Contains(result.Stdout(), "==> repoA (feature)\nrepoA\n\n==> sub/deep/repoB (feature)\nrepoB\n"))

	// stdin is connected to the command, so it can be interactive.
	cli := gocmdtester.FromPath(t, mainGo, gocmdtester.WithWorkingDir(target), gocmdtester.WithStdin(strings.NewReader("typed\n")))
	result = cli.Run("exec", "head", "-1")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Assert(t, cmp.Contains(result.Stdout(), "==> repoA (feature)\ntyped\n"))

	// stderr is connected directly too, and failures are still counted.
	result = newCLI(t, target).Run("exec", "sh", "-c", "echo oops >&2; exit 3")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Equal(t, strings.Count(result.Stderr(), "oops\n"), 4)
	assert.Assert(t, cmp.Contains(result.Stderr(), "sh failed in 4 of 4 repositories"))
}

func TestExec_ParallelCapturesOutput(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	// Headers only for repositories that produced output.
	result := newCLI(t, target).Run("exec", "--parallel", "true")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, result.Stdout(), "")

	result = newCLI(t, target).Run("exec", "--parallel", "sh", "-c", "basename \"$PWD\"")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Assert(t, cmp.Contains(result.Stdout(), "==> repoA (feature)\nrepoA\n\n==> sub/deep/repoB (feature)\nrepoB\n"))

	// stdin is not connected.
	cli := gocmdtester.FromPath(t, mainGo, gocmdtester.WithWorkingDir(target), gocmdtester.WithStdin(strings.NewReader("typed\n")))
	result = cli.Run("exec", "--parallel", "head", "-1")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, result.Stdout(), "")

	// Both modes at once is an error.
	result = newCLI(t, target).Run("exec", "--parallel", "--no-parallel", "true")
	assert.Equal(t, result.ExitCode(), 2)
	assert.Assert(t, cmp.Contains(result.Stderr(), "cannot be combined"))
}

func TestExec_ConfigSetsTheDefaultMode(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)
	assert.NilError(t, os.WriteFile(filepath.Join(f.Source, ".workyard", "config.yaml"), []byte("exec:\n  parallel: true\n"), 0o644))

	result := newCLI(t, target).Run("exec", "true")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, result.Stdout(), "", "parallel mode prints no header without output")

	result = newCLI(t, target).Run("exec", "--no-parallel", "true")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, strings.Count(result.Stdout(), "==>"), 4)

	// Unknown keys are rejected.
	assert.NilError(t, os.WriteFile(filepath.Join(f.Source, ".workyard", "config.yaml"), []byte("exec:\n  serial: true\n"), 0o644))

	result = newCLI(t, target).Run("exec", "true")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, cmp.Contains(result.Stderr(), "field serial not found"))
}

func TestExec_ExitCodeReflectsFailures(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	// The branch "existing" only exists in repoA, so git fails in the others.
	result := newCLI(t, target).Run("exec", "--parallel", "git", "rev-parse", "--verify", "--quiet", "refs/heads/existing")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, cmp.Contains(result.Stderr(), "git failed in 3 of 4 repositories"))
	assert.Equal(t, strings.Count(result.Stdout(), "==>"), 1, "only repoA printed anything")
	assert.Assert(t, cmp.Contains(result.Stdout(), "==> repoA (feature)"))

	// stderr from the command is forwarded.
	result = newCLI(t, target).Run("exec", "--parallel", "git", "log", "no-such-ref")
	assert.Equal(t, result.ExitCode(), 1)
	assert.Assert(t, cmp.Contains(result.Stderr(), "unknown revision"))

	// A command that cannot be started at all is a failure too, in both modes.
	for _, mode := range []string{"--parallel", "--no-parallel"} {
		result = newCLI(t, target).Run("exec", mode, "no-such-program-xyz")
		assert.Equal(t, result.ExitCode(), 1, mode)
		assert.Equal(t, strings.Count(result.Stderr(), "warning: "), 4, mode)
		assert.Assert(t, cmp.Contains(result.Stderr(), "no-such-program-xyz failed in 4 of 4 repositories"), mode)
	}
}

func TestExec_RequiresACommand(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	result := newCLI(t, target).Run("exec")
	assert.Equal(t, result.ExitCode(), 2)
	assert.Assert(t, cmp.Contains(result.Stderr(), "no command given"))
}

func TestExec_UnknownOptionOutsideFanOutIsAnError(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	result := newCLI(t, target).Run("list", "--bogus")
	assert.Equal(t, result.ExitCode(), 2)
	assert.Assert(t, cmp.Contains(result.Stderr(), "unknown flag"))
}

func TestExec_CommandRunsAsTheUserWould(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	realTarget, err := filepath.EvalSymlinks(target)
	assert.NilError(t, err)

	for _, mode := range []string{"--parallel", "--no-parallel"} {
		// PWD follows the working directory, and the environment is inherited
		// as is, GIT_* variables included.
		cli := newCLI(t, target, "GIT_AUTHOR_NAME", "Someone Else", "MY_VAR", "kept")
		result := cli.Run("exec", mode, "sh", "-c", "echo $PWD $GIT_AUTHOR_NAME $MY_VAR")
		assert.Equal(t, result.ExitCode(), 0, result.Stderr())

		for _, repo := range fixtureRepos {
			assert.Assert(t, cmp.Contains(result.Stdout(), "\n"+filepath.Join(realTarget, repo)+" Someone Else kept\n"), "%s %s", mode, repo)
		}

		// git's color settings are left alone: with a pipe on stdout it stays
		// plain unless the user asks otherwise.
		result = newCLI(t, target).Run("exec", mode, "git", "log", "--oneline", "-1")
		assert.Equal(t, result.ExitCode(), 0, result.Stderr())
		assert.Assert(t, !strings.Contains(result.Stdout(), "\x1b["), mode)

		result = newCLI(t, target).Run("exec", mode, "git", "log", "--color=always", "--oneline", "-1")
		assert.Equal(t, result.ExitCode(), 0, result.Stderr())
		assert.Equal(t, strings.Count(result.Stdout(), "\x1b[33m"), 4, mode)
	}
}
