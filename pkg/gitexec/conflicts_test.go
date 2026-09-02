package gitexec

import (
	"testing"

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

func TestConflictMarkerSize(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	setupRepo(t, repoPath, `
		printf 'big.txt conflict-marker-size=12\nbogus.txt conflict-marker-size=abc\nflag.txt conflict-marker-size\n' > .gitattributes
		git add .gitattributes
		git commit -m attrs
	`)

	repo := WithRepo(repoPath)

	for _, tc := range []struct {
		path string
		want int
	}{
		{"big.txt", 12},
		{"plain.txt", DefaultConflictMarkerSize},
		{"bogus.txt", DefaultConflictMarkerSize},
		{"flag.txt", DefaultConflictMarkerSize},
		{"caf\u00e9 menu.txt", DefaultConflictMarkerSize},
	} {
		size, err := repo.ConflictMarkerSize(tc.path)
		assert.NilError(t, err, tc.path)
		assert.Equal(t, size, tc.want, tc.path)
	}
}
