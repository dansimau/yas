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
	first := createYard(t, f)
	second := createYard(t, f, "--branch", "feature2")

	// The listing shows the real paths recorded in the metadata.
	firstPath := openYard(t, first).Meta.Target
	secondPath := openYard(t, second).Meta.Target

	// From inside a yard, the yards of its source are listed and it is marked.
	result := newCLI(t, filepath.Join(second, "wt", "main")).Run("ls")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())

	lines := strings.Split(strings.TrimSpace(result.Stdout()), "\n")
	assert.Equal(t, len(lines), 2, result.Stdout())

	var current, other []string

	for _, line := range lines {
		fields := strings.Fields(line)
		if fields[0] == "*" {
			current = fields
		} else {
			other = fields
		}
	}

	assert.DeepEqual(t, current[:5], []string{"*", secondPath, "feature2", "4", "repos"})
	assert.DeepEqual(t, other[:4], []string{firstPath, "feature", "4", "repos"})
	assert.Assert(t, !strings.Contains(result.Stdout(), "(missing)"))
	assert.Assert(t, !strings.Contains(result.Stdout(), "(incomplete)"))

	// From the source itself, the same yards are listed, none marked.
	result = newCLI(t, f.Source).Run("list")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Assert(t, cmp.Contains(result.Stdout(), firstPath))
	assert.Assert(t, cmp.Contains(result.Stdout(), secondPath))
	assert.Assert(t, !strings.Contains(result.Stdout(), "*"))

	// A yard whose directory was deleted by hand is flagged.
	assert.NilError(t, os.Chmod(filepath.Join(first, "readonly"), 0o755))
	assert.NilError(t, os.RemoveAll(first))

	result = newCLI(t, t.TempDir()).Run("ls", "--source", f.Source)
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	lines = strings.Split(strings.TrimSpace(result.Stdout()), "\n")
	assert.Equal(t, len(lines), 2, result.Stdout())
	assert.DeepEqual(t, strings.Fields(lines[0])[:4], []string{firstPath, "feature", "4", "repos"})
	assert.Assert(t, cmp.Contains(lines[0], "(missing)"))
	assert.Assert(t, !strings.Contains(lines[1], "(missing)"))
}

func TestList_NoYards(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	result := newCLI(t, dir).Run("ls")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())
	assert.Equal(t, result.Stdout(), "")
	assert.Assert(t, cmp.Contains(result.Stderr(), "No workyards created from "))
}

func TestList_JSON(t *testing.T) {
	t.Parallel()

	f := setupFixture(t)
	target := createYard(t, f)

	result := newCLI(t, target).Run("list", "--json")
	assert.Equal(t, result.ExitCode(), 0, result.Stderr())

	var yards []workyard.Metadata
	assert.NilError(t, json.Unmarshal([]byte(result.Stdout()), &yards))
	assert.Equal(t, len(yards), 1)

	meta := yards[0]
	assert.Equal(t, meta.Version, workyard.MetadataVersion)
	assert.Equal(t, meta.Branch, "feature")
	assert.Assert(t, meta.Complete)
	assert.Equal(t, len(meta.Repos), 4)
	assert.Equal(t, meta.Repos[0].Path, "repoA")
	assert.Assert(t, strings.HasSuffix(meta.Source, "/src"))
	assert.Assert(t, meta.GitVersion != "")
}
