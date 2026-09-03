package gitexec

import (
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"gotest.tools/v3/assert"
)

// setupConflictedRepo leaves the repo in the middle of a rebase with a
// conflict in the given file and a second, ASCII-named file.
func setupConflictedRepo(t *testing.T, repoPath, weirdFile string) {
	t.Helper()

	setupRepo(t, repoPath, `
		git config commit.gpgsign false
		git checkout -B main
		printf 'base\n' > '`+weirdFile+`'
		printf 'base\n' > plain.txt
		git add -A
		git commit -m base

		git checkout -b topic
		printf 'topic\n' > '`+weirdFile+`'
		printf 'topic\n' > plain.txt
		git commit -am topic

		git checkout main
		printf 'main\n' > '`+weirdFile+`'
		printf 'main\n' > plain.txt
		git commit -am main

		git checkout topic
		git rebase main || true
	`)
}

func TestUnmergedFiles_PreservesQuotedPaths(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	// A non-ASCII name that git would C-quote in line-based output, plus a
	// space for good measure.
	weirdFile := "caf\u00e9 menu.txt"
	setupConflictedRepo(t, repoPath, weirdFile)

	files, err := WithRepo(repoPath).UnmergedFiles()
	assert.NilError(t, err)
	assert.DeepEqual(t, files, []string{weirdFile, "plain.txt"})
}

func TestUnmergedFiles_NoneWhenClean(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	setupRepo(t, repoPath, "")

	files, err := WithRepo(repoPath).UnmergedFiles()
	assert.NilError(t, err)
	assert.Equal(t, len(files), 0)
}

func TestStatusEntries(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	setupRepo(t, repoPath, `
		git config commit.gpgsign false
		printf 'a\n' > tracked.txt
		printf 'b\n' > renamed-from.txt
		printf 'd\n' > removed.txt
		mkdir dir
		printf 'c\n' > dir/nested.txt
		git add -A
		git commit -m files

		printf 'changed\n' > tracked.txt
		git mv renamed-from.txt renamed-to.txt
		git rm -q removed.txt
		printf 'new\n' > 'untracked file.txt'
		printf 'new\n' > dir/also-new.txt
	`)

	entries, err := WithRepo(repoPath).StatusEntries()
	assert.NilError(t, err)

	assert.DeepEqual(t, entries, map[string]string{
		"tracked.txt":        " M",
		"renamed-to.txt":     "R ",
		"removed.txt":        "D ",
		"untracked file.txt": "??",
		"dir/also-new.txt":   "??",
	})
}

func TestAdd_LiteralPathspec(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	setupRepo(t, repoPath, `
		printf 'x\n' > ':(glob)*.txt'
		printf 'y\n' > other.txt
	`)

	repo := WithRepo(repoPath)

	// Without literal pathspecs, git would expand the glob and stage
	// other.txt as well.
	assert.NilError(t, repo.Add(":(glob)*.txt"))

	out, err := repo.rawOutput("git", "diff", "--cached", "--name-only", "-z")
	assert.NilError(t, err)
	assert.DeepEqual(t, splitNUL(out), []string{":(glob)*.txt"})

	// A path that no longer exists is staged as a deletion.
	testutil.ExecOrFail(t, repoPath, `
		git add other.txt
		git commit -qm other
		rm other.txt
	`)
	assert.NilError(t, repo.Add("other.txt"))

	out, err = repo.rawOutput("git", "diff", "--cached", "--name-status", "-z")
	assert.NilError(t, err)
	assert.DeepEqual(t, splitNUL(out), []string{"D", "other.txt"})
}
