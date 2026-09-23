package tests

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

const (
	keyUp     = "\x1b[A"
	keyDown   = "\x1b[B"
	keyEnter  = "\r"
	keyEscape = "\x1b"
)

// ptyOutput collects what the program writes to its terminal.
type ptyOutput struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (o *ptyOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.buf.Write(p)
}

func (o *ptyOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.buf.String()
}

// runSwitch runs "workyard switch" in workingDir on a pseudo-terminal, with
// the shell hook's file at shellExecPath, pressing each key once the selector
// is showing. It returns the exit code and the terminal output.
func runSwitch(t *testing.T, workingDir string, shellExecPath string, keys ...string) (int, string) {
	t.Helper()

	tester := newCLI(t, workingDir)

	cmd := exec.Command(tester.BinaryPath(), "switch")
	cmd.Dir = workingDir

	cmd.Env = append(os.Environ(), "GOCOVERDIR="+tester.CoverageDir(), "WORKYARD_SHELL_EXEC="+shellExecPath)

	terminal, err := pty.Start(cmd)
	assert.NilError(t, err)

	defer terminal.Close()

	out := &ptyOutput{}
	copied := make(chan struct{})

	go func() {
		_, _ = io.Copy(out, terminal)

		close(copied)
	}()

	// The header is printed once the terminal is in raw mode, so keys typed
	// after it are read one by one.
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "Choose workyard to switch to:") {
		assert.Assert(t, time.Now().Before(deadline), "selector did not appear:\n%s", out.String())
		time.Sleep(10 * time.Millisecond)
	}

	for _, key := range keys {
		_, err := terminal.WriteString(key)
		assert.NilError(t, err)
		time.Sleep(50 * time.Millisecond)
	}

	err = cmd.Wait()

	<-copied

	exitErr := &exec.ExitError{}
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), out.String()
	}

	assert.NilError(t, err)

	return 0, out.String()
}

// sortedTargets returns the real paths of the yards in the order "workyard
// list" shows them.
func sortedTargets(t *testing.T, yards ...string) []string {
	t.Helper()

	var targets []string
	for _, yard := range yards {
		targets = append(targets, openYard(t, yard).Meta.Target)
	}

	slices.Sort(targets)

	return targets
}

func TestSwitch_ChangesDirectory(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	targets := sortedTargets(t, createYard(t, f), createYard(t, f, "--branch", "feature2"))

	// From the source no yard is current, so the cursor starts on the first.
	shellExec := filepath.Join(t.TempDir(), "shell-exec")
	exitCode, out := runSwitch(t, f.Source, shellExec, keyDown, keyEnter)
	assert.Equal(t, exitCode, 0, out)
	assert.Assert(t, cmp.Contains(out, targets[0]))
	assert.Assert(t, cmp.Contains(out, targets[1]))
	assertFileContent(t, shellExec, "cd "+targets[1]+"\necho 'Switched to workyard: "+targets[1]+"'\n")

	// From inside a yard the cursor starts on it.
	shellExec = filepath.Join(t.TempDir(), "shell-exec")
	exitCode, out = runSwitch(t, filepath.Join(targets[1], "repoA"), shellExec, keyUp, keyEnter)
	assert.Equal(t, exitCode, 0, out)
	assertFileContent(t, shellExec, "cd "+targets[0]+"\necho 'Switched to workyard: "+targets[0]+"'\n")
}

func TestSwitch_Cancel(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	createYard(t, f)

	shellExec := filepath.Join(t.TempDir(), "shell-exec")
	exitCode, out := runSwitch(t, f.Source, shellExec, keyEscape)
	assert.Equal(t, exitCode, 0, out)
	assertNotExists(t, shellExec)
}

func TestSwitch_MissingYard(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	yard := createYard(t, f)
	target := openYard(t, yard).Meta.Target

	assert.NilError(t, os.Chmod(filepath.Join(yard, "readonly"), 0o755))
	assert.NilError(t, os.RemoveAll(yard))

	shellExec := filepath.Join(t.TempDir(), "shell-exec")
	exitCode, out := runSwitch(t, f.Source, shellExec, keyEnter)
	assert.Equal(t, exitCode, 1, out)
	assert.Assert(t, cmp.Contains(out, "(missing)"))
	assert.Assert(t, cmp.Contains(out, "ERROR: cannot switch to workyard "+target))
	assertNotExists(t, shellExec)
}

func TestSwitch_HookNotInstalled(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	createYard(t, f)

	result := newCLI(t, f.Source).Run("switch")
	assert.Equal(t, result.ExitCode(), 2, result.Stderr())
	assert.Assert(t, cmp.Contains(result.Stderr(), "WORKYARD_SHELL_EXEC environment variable not set"))
	assert.Assert(t, cmp.Contains(result.Stderr(), `eval "$(workyard hook zsh)"`))
}

func TestSwitch_NoYards(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	for _, command := range []string{"switch", "sw"} {
		result := newCLI(t, dir, "WORKYARD_SHELL_EXEC", filepath.Join(dir, "shell-exec")).Run(command)
		assert.Equal(t, result.ExitCode(), 1, result.Stderr())
		assert.Assert(t, cmp.Contains(result.Stderr(), "ERROR: no workyards created from "))
	}
}

func TestSwitch_UnexpectedArguments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	result := newCLI(t, dir, "WORKYARD_SHELL_EXEC", filepath.Join(dir, "shell-exec")).Run("switch", "foo")
	assert.Equal(t, result.ExitCode(), 2, result.Stderr())
	assert.Assert(t, cmp.Contains(result.Stderr(), "unexpected arguments: foo"))
}

func TestHook(t *testing.T) {
	t.Parallel()

	for _, shell := range []string{"bash", "zsh"} {
		result := newCLI(t, t.TempDir()).Run("hook", shell)
		assert.Equal(t, result.ExitCode(), 0, result.Stderr())
		assert.Assert(t, cmp.Contains(result.Stdout(), "# workyard shell hook for "+shell))
		assert.Assert(t, cmp.Contains(result.Stdout(), `export WORKYARD_SHELL_EXEC="$workyard_shell_exec_file"`))
		assert.Assert(t, cmp.Contains(result.Stdout(), `command workyard "$@"`))
		assert.Assert(t, !strings.Contains(result.Stdout(), "YAS_SHELL_EXEC"))
	}
}
