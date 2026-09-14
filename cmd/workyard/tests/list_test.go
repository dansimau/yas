package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/workyard"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestList_Plain(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	assert.NilError(t, os.WriteFile(filepath.Join(target, "repoA", "file.txt"), []byte("changed\n"), 0o644))

	result := newCLI(t, filepath.Join(target, "wt", "main")).Run("ls")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())

	lines := strings.Split(strings.TrimSpace(result.Stdout()), "\n")
	assert.Assert(t, cmp.Contains(lines[0], "branch feature"))
	assert.Equal(t, len(lines), 5, result.Stdout())

	shortHash := mustOutput(t, filepath.Join(f.Source, "repoA"), "git", "rev-parse", "--short", "main")

	repoA := strings.Fields(lines[1])
	assert.DeepEqual(t, repoA, []string{"repoA", "feature", shortHash, "*"})

	repoB := strings.Fields(lines[2])
	assert.Equal(t, repoB[0], "sub/deep/repoB")
	assert.Equal(t, repoB[1], "feature")
	assert.Equal(t, len(repoB), 3, "clean repositories have no dirty marker")

	other := strings.Fields(lines[4])
	assert.DeepEqual(t, other[:2], []string{"wt/other", "HEAD"})
}

func TestList_JSON(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	result := newCLI(t, target).Run("list", "--json")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())

	var meta workyard.Metadata
	assert.NilError(t, json.Unmarshal([]byte(result.Stdout()), &meta))
	assert.Equal(t, meta.Version, workyard.MetadataVersion)
	assert.Equal(t, meta.Branch, "feature")
	assert.Assert(t, meta.Complete)
	assert.Equal(t, len(meta.Repos), 4)
	assert.Equal(t, meta.Repos[0].Path, "repoA")
	assert.Assert(t, strings.HasSuffix(meta.Source, "/src"))
	assert.Assert(t, meta.GitVersion != "")
}
