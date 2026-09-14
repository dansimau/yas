package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestDiff_ShowsChangesInTheRightRepository(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	assert.NilError(t, os.WriteFile(filepath.Join(target, "repoA", "file.txt"), []byte("changed\n"), 0o644))

	result := newCLI(t, filepath.Join(target, "wt")).Run("diff", "--header")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())

	stdout := result.Stdout()
	assert.Equal(t, strings.Count(stdout, "==>"), 4, "every repository gets a header")

	repoA := between(t, stdout, "==> repoA", "==> sub/deep/repoB")
	assert.Assert(t, cmp.Contains(repoA, "-content"))
	assert.Assert(t, cmp.Contains(repoA, "+changed"))

	after := between(t, stdout, "==> sub/deep/repoB", "")
	assert.Assert(t, !strings.Contains(after, "+changed"), "diff leaked into another repository")

	// Options are passed to git diff; --quiet hides repositories without output.
	result = newCLI(t, target).Run("diff", "--header", "--quiet", "--stat")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, strings.Count(result.Stdout(), "==>"), 1)
	assert.Assert(t, cmp.Contains(result.Stdout(), "1 file changed"))
}

func TestDiff_NoChangesNoOutput(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	result := newCLI(t, target).Run("diff")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, result.Stdout(), "")
}

func TestDiff_ColorIsNotForcedWithoutTerminal(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)
	assert.NilError(t, os.WriteFile(filepath.Join(target, "repoA", "file.txt"), []byte("changed\n"), 0o644))

	result := newCLI(t, target).Run("diff")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Assert(t, !strings.Contains(result.Stdout(), "\x1b["), "no escape codes when stdout is not a terminal")

	result = newCLI(t, target).Run("--color=always", "diff")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Assert(t, cmp.Contains(result.Stdout(), "\x1b["))
}
