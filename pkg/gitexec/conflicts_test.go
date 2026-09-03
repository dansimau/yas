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

func TestStatusEntries(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	setupRepo(t, repoPath, `
		git config commit.gpgsign false
		printf 'a\n' > tracked.txt
		printf 'b\n' > renamed-from.txt
		printf 'secret.env\n' > .gitignore
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
		printf 'shh\n' > secret.env
		ln -s tracked.txt link
	`)

	repo := WithRepo(repoPath)

	entries, err := repo.StatusEntries()
	assert.NilError(t, err)

	statuses := map[string]string{}
	for path, entry := range entries {
		statuses[path] = entry.Status
	}

	assert.DeepEqual(t, statuses, map[string]string{
		"tracked.txt":        " M",
		"renamed-to.txt":     "R ",
		"removed.txt":        "D ",
		"untracked file.txt": "??",
		"dir/also-new.txt":   "??",
		"secret.env":         "!!",
		"link":               "??",
	})

	// Fingerprints describe the working-tree file...
	assert.Equal(t, entries["tracked.txt"].Size, int64(len("changed\n")))
	assert.Assert(t, entries["tracked.txt"].ModTime != 0)
	assert.Assert(t, entries["tracked.txt"].Mode.IsRegular())
	assert.Equal(t, entries["link"].LinkTarget, "tracked.txt")
	// ...and are zero for a path with no working-tree file.
	assert.Equal(t, entries["removed.txt"], StatusEntry{Status: "D "})

	// Overwriting an untracked or ignored file leaves its status code alone
	// but changes its fingerprint.
	testutil.ExecOrFail(t, repoPath, `
		printf 'overwritten\n' > 'untracked file.txt'
		printf 'overwritten\n' > secret.env
	`)

	after, err := repo.StatusEntries()
	assert.NilError(t, err)
	assert.Equal(t, after["untracked file.txt"].Status, "??")
	assert.Assert(t, after["untracked file.txt"] != entries["untracked file.txt"])
	assert.Equal(t, after["secret.env"].Status, "!!")
	assert.Assert(t, after["secret.env"] != entries["secret.env"])
	assert.Equal(t, after["tracked.txt"], entries["tracked.txt"])
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
