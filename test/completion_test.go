package test

import (
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"gotest.tools/v3/assert"
)

func TestCompletionBash(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	result := cli.Run("completion", "bash")
	assert.Equal(t, result.ExitCode(), 0)
	assert.Assert(t, result.StdoutContains("bash completion for yas"), "Expected bash completion script, got: %s", result.Stdout())
	assert.Assert(t, result.StdoutContains("_yas_completion"), "Expected completion function, got: %s", result.Stdout())
	assert.Assert(t, result.StdoutContains("complete -F _yas_completion yas"), "Expected complete command, got: %s", result.Stdout())
}

func TestCompletionZsh(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	result := cli.Run("completion", "zsh")
	assert.Equal(t, result.ExitCode(), 0)
	assert.Assert(t, result.StdoutContains("#compdef yas"), "Expected zsh completion script, got: %s", result.Stdout())
	assert.Assert(t, result.StdoutContains("_yas"), "Expected completion function, got: %s", result.Stdout())
}

func TestCompletionFish(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	result := cli.Run("completion", "fish")
	assert.Equal(t, result.ExitCode(), 0)
	assert.Assert(t, result.StdoutContains("fish completion for yas"), "Expected fish completion script, got: %s", result.Stdout())
	assert.Assert(t, result.StdoutContains("complete -c yas"), "Expected complete commands, got: %s", result.Stdout())
}

func TestCompletionHelp(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
		gocmdtester.WithWorkingDir(tempDir),
	)

	result := cli.Run("completion", "--help")
	assert.Equal(t, result.ExitCode(), 0)
	output := result.Stderr()

	// Check that all shell types are documented
	assert.Assert(t, strings.Contains(output, "bash"), "Expected bash in help output")
	assert.Assert(t, strings.Contains(output, "zsh"), "Expected zsh in help output")
	assert.Assert(t, strings.Contains(output, "fish"), "Expected fish in help output")
}
