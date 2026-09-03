package conflictresolver_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/conflictresolver"
	"github.com/dansimau/yas/pkg/testutil"
	"gotest.tools/v3/assert"
)

func TestRegistry(t *testing.T) {
	t.Parallel()

	assert.Assert(t, conflictresolver.IsValid("none"))
	assert.Assert(t, conflictresolver.IsValid("claude"))
	assert.Assert(t, !conflictresolver.IsValid("bogus"))
	assert.Assert(t, !conflictresolver.IsValid(""))

	assert.DeepEqual(t, conflictresolver.Names(), []string{"claude", "none"})

	r, err := conflictresolver.New("claude")
	assert.NilError(t, err)
	assert.Equal(t, r.Name(), "claude")

	_, err = conflictresolver.New("none")
	assert.ErrorContains(t, err, `unknown conflict resolver "none"`)

	_, err = conflictresolver.New("bogus")
	assert.ErrorContains(t, err, "valid values: claude, none")
}

func TestRegister_Duplicate(t *testing.T) {
	t.Parallel()

	defer func() {
		assert.Assert(t, recover() != nil, "registering a duplicate name should panic")
	}()

	conflictresolver.Register("claude", func() conflictresolver.Resolver { return nil })
}

func TestRegister_ReservedName(t *testing.T) {
	t.Parallel()

	defer func() {
		assert.Assert(t, recover() != nil, "registering 'none' should panic")
	}()

	conflictresolver.Register("none", func() conflictresolver.Resolver { return nil })
}

func TestHasConflictMarkers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		assert.NilError(t, os.WriteFile(path, []byte(content), 0o644))

		return path
	}

	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"clean", "line1\nline2\n", false},
		{"full-conflict", "a\n<<<<<<< HEAD\nb\n=======\nc\n>>>>>>> abc123 (commit)\n", true},
		{"only-end-marker", "a\nb\n>>>>>>> theirs\n", true},
		{"diff3-marker", "a\n||||||| base\nb\n", true},
		{"bare-start-marker", "<<<<<<<\n", true},
		{"no-trailing-newline", "a\n<<<<<<< HEAD", true},
		// Markers written with a custom conflict-marker-size are longer runs
		// and count regardless of their length.
		{"twelve-chars", "a\n<<<<<<<<<<<< HEAD\nb\n============\nc\n>>>>>>>>>>>> theirs\n", true},
		{"eight-chars", "<<<<<<<< HEAD\n", true},
		{"very-long", strings.Repeat(">", 300) + " theirs\n", true},
		// "=======" on its own is legitimate content (e.g. a Markdown setext
		// heading underline) and must not count as a marker.
		{"setext-heading", "Title\n=======\nbody\n", false},
		// Runs shorter than git's default, text not at the start of a line,
		// and runs not followed by a space are not markers.
		{"six-chars", "<<<<<< HEAD\n", false},
		{"marker-not-at-start", "x <<<<<<< y\n", false},
		{"run-then-text", "<<<<<<<HEAD\n", false},
		{"markdown-quote", "> quoted\n>> nested\n", false},
		{"python-prompt", ">>> print(1)\n", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := conflictresolver.HasConflictMarkers(write(tc.name, tc.content))
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}

	t.Run("missing-file", func(t *testing.T) {
		t.Parallel()

		got, err := conflictresolver.HasConflictMarkers(filepath.Join(dir, "does-not-exist"))
		assert.NilError(t, err)
		assert.Assert(t, !got)
	})

	t.Run("non-regular-files", func(t *testing.T) {
		t.Parallel()

		// Directories (conflicted gitlinks) and symlinks hold no markers,
		// even when the link target does.
		assert.NilError(t, os.Mkdir(filepath.Join(dir, "submodule"), 0o755))

		got, err := conflictresolver.HasConflictMarkers(filepath.Join(dir, "submodule"))
		assert.NilError(t, err)
		assert.Assert(t, !got)

		write("linked", "<<<<<<< HEAD\n")
		assert.NilError(t, os.Symlink("linked", filepath.Join(dir, "link")))

		got, err = conflictresolver.HasConflictMarkers(filepath.Join(dir, "link"))
		assert.NilError(t, err)
		assert.Assert(t, !got)
	})
}

func TestHasConflictMarkers_LongLines(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// A 17 MiB line with no newline (as in a large binary or minified file),
	// followed by an ordinary conflict. bufio.Scanner would give up on the
	// first line; the scan must stream past it.
	path := filepath.Join(dir, "long.txt")
	f, err := os.Create(path)
	assert.NilError(t, err)

	chunk := []byte(strings.Repeat("x", 1024*1024))
	for range 17 {
		_, err = f.Write(chunk)
		assert.NilError(t, err)
	}

	_, err = f.WriteString("\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> topic\n")
	assert.NilError(t, err)
	assert.NilError(t, f.Close())

	hasMarkers, err := conflictresolver.HasConflictMarkers(path)
	assert.NilError(t, err)
	assert.Assert(t, hasMarkers)

	// Only the long line, no newline at all: not an error, just no markers.
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "blob.bin"), chunk, 0o644))

	hasMarkers, err = conflictresolver.HasConflictMarkers(filepath.Join(dir, "blob.bin"))
	assert.NilError(t, err)
	assert.Assert(t, !hasMarkers)

	before, err := conflictresolver.SnapshotFiles(dir, []string{"long.txt", "blob.bin"})
	assert.NilError(t, err)
	assert.Assert(t, before["long.txt"].HasMarkers)
	assert.Assert(t, !before["blob.bin"].HasMarkers)
}

func TestFilesWithConflictMarkers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "clean.txt"), []byte("ok\n"), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "bad.txt"), []byte("<<<<<<< HEAD\nx\n=======\ny\n>>>>>>> z\n"), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "big.txt"), []byte("<<<<<<<<<<<< HEAD\nx\n============\ny\n>>>>>>>>>>>> z\n"), 0o644))

	remaining, err := conflictresolver.FilesWithConflictMarkers(dir, []string{"clean.txt", "bad.txt", "big.txt", "deleted.txt"})
	assert.NilError(t, err)
	assert.DeepEqual(t, remaining, []string{"bad.txt", "big.txt"})

	assert.NilError(t, os.WriteFile(filepath.Join(dir, "big.txt"), []byte("resolved\n"), 0o644))

	remaining, err = conflictresolver.FilesWithConflictMarkers(dir, []string{"clean.txt", "bad.txt", "big.txt"})
	assert.NilError(t, err)
	assert.DeepEqual(t, remaining, []string{"bad.txt"})
}

func TestClaude_Args(t *testing.T) {
	t.Parallel()

	c := &conflictresolver.Claude{Binary: "claude"}
	req := conflictresolver.Request{
		Dir:           "/repo",
		Files:         []string{"a.go", "b.go"},
		Branch:        "feature",
		Onto:          "main",
		CommitSubject: "Add thing",
	}

	args := c.Args(req)

	assert.Equal(t, args[0], "claude")
	assert.Equal(t, args[1], "-p")
	assert.Equal(t, args[2], conflictresolver.Prompt(req))
	assert.DeepEqual(t, args[3:], []string{
		"--permission-mode", "acceptEdits",
		"--allowedTools", "Read,Edit,Write,Grep,Glob,Bash(git diff:*),Bash(git log:*),Bash(git show:*),Bash(git status:*)",
	})

	prompt := conflictresolver.Prompt(req)
	for _, want := range []string{
		"`feature`", "`main`", `"Add thing"`, "- a.go", "- b.go", "Do NOT run git add",
		// HEAD is the partially rebased result, not just the target branch.
		"rebased result so far: `main` plus any commits from `feature` that have already been replayed",
	} {
		assert.Assert(t, strings.Contains(prompt, want), "prompt should contain %q:\n%s", want, prompt)
	}

	// Without a commit subject the line is omitted entirely.
	req.CommitSubject = ""
	assert.Assert(t, !strings.Contains(conflictresolver.Prompt(req), "The commit currently being replayed is:"))
}

func TestClaude_CheckAvailable(t *testing.T) {
	t.Parallel()

	missing := &conflictresolver.Claude{Binary: "yas-test-binary-that-does-not-exist"}
	err := missing.CheckAvailable()
	assert.ErrorContains(t, err, `requires the "yas-test-binary-that-does-not-exist" command`)

	// Any binary that certainly exists will do to exercise the success path.
	present := &conflictresolver.Claude{Binary: "sh"}
	assert.NilError(t, present.CheckAvailable())
}

func TestClaude_Resolve(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	assert.NilError(t, os.MkdirAll(binDir, 0o755))

	// A stand-in for the claude CLI that records its working directory and
	// arguments, then "resolves" the conflict.
	script := `#!/bin/sh
pwd > "$0.cwd"
printf '%s\n' "$@" > "$0.args"
echo resolved > conflicted.txt
`
	assert.NilError(t, os.WriteFile(filepath.Join(binDir, "fake-claude"), []byte(script), 0o755))

	workDir := filepath.Join(dir, "work")
	assert.NilError(t, os.MkdirAll(workDir, 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(workDir, "conflicted.txt"), []byte("<<<<<<< HEAD\n"), 0o644))

	c := &conflictresolver.Claude{Binary: filepath.Join(binDir, "fake-claude")}
	err := c.Resolve(conflictresolver.Request{Dir: workDir, Files: []string{"conflicted.txt"}, Branch: "b", Onto: "main"})
	assert.NilError(t, err)

	cwd, err := os.ReadFile(filepath.Join(binDir, "fake-claude.cwd"))
	assert.NilError(t, err)

	// Resolve realpath on both sides in case the temp dir is symlinked.
	wantCwd, err := filepath.EvalSymlinks(workDir)
	assert.NilError(t, err)
	gotCwd, err := filepath.EvalSymlinks(strings.TrimSpace(string(cwd)))
	assert.NilError(t, err)
	assert.Equal(t, gotCwd, wantCwd, "resolver must run in the rebase directory")

	args, err := os.ReadFile(filepath.Join(binDir, "fake-claude.args"))
	assert.NilError(t, err)
	assert.Assert(t, strings.HasPrefix(string(args), "-p\n"), "args: %s", args)

	content, err := os.ReadFile(filepath.Join(workDir, "conflicted.txt"))
	assert.NilError(t, err)
	assert.Equal(t, string(content), "resolved\n")

	// A failing tool surfaces as an error.
	failing := `#!/bin/sh
echo "boom" >&2
exit 3
`
	assert.NilError(t, os.WriteFile(filepath.Join(binDir, "failing-claude"), []byte(failing), 0o755))

	f := &conflictresolver.Claude{Binary: filepath.Join(binDir, "failing-claude")}
	err = f.Resolve(conflictresolver.Request{Dir: workDir, Files: []string{"conflicted.txt"}})
	assert.ErrorContains(t, err, "exited with an error")
}

// Not parallel: t.Setenv mutates the process environment, which would race
// with other tests that launch git.
func TestClaude_Resolve_StripsGitEnv(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	assert.NilError(t, os.MkdirAll(binDir, 0o755))

	workDir := filepath.Join(dir, "work")
	assert.NilError(t, os.MkdirAll(workDir, 0o755))

	// GIT_* variables must not leak into the resolver, otherwise its git
	// commands could inspect a different repository than req.Dir.
	envScript := `#!/bin/sh
printf '%s|%s|%s' "$GIT_DIR" "$GIT_WORK_TREE" "$KEEP_ME" > "$0.env"
`
	assert.NilError(t, os.WriteFile(filepath.Join(binDir, "env-claude"), []byte(envScript), 0o755))

	t.Setenv("GIT_DIR", "/elsewhere/.git")
	t.Setenv("GIT_WORK_TREE", "/elsewhere")
	t.Setenv("KEEP_ME", "kept")

	e := &conflictresolver.Claude{Binary: filepath.Join(binDir, "env-claude")}
	assert.NilError(t, e.Resolve(conflictresolver.Request{Dir: workDir, Files: []string{"conflicted.txt"}}))

	env, err := os.ReadFile(filepath.Join(binDir, "env-claude.env"))
	assert.NilError(t, err)
	assert.Equal(t, string(env), "||kept", "GIT_* stripped, other variables preserved")
}

func TestSnapshotAndUnverifiableFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, content string) {
		assert.NilError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}

	write("textual.txt", "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> c\n")
	write("binary.bin", "\x00\x01\x02")
	write("kept.txt", "content from the modifying side\n")
	write("touched.txt", "as git left it\n")
	write("chmodded.txt", "same bytes on both sides\n")
	write("removed.txt", "will be deleted by the resolver\n")
	// "gone.txt" never existed in the working tree (deleted side of a
	// modify/delete conflict that git resolved towards the deletion).
	// "submodule" is a directory: a conflicted gitlink.
	assert.NilError(t, os.Mkdir(filepath.Join(dir, "submodule"), 0o755))
	write("submodule/inner.txt", "checked-out submodule content\n")

	files := []string{"textual.txt", "binary.bin", "kept.txt", "touched.txt", "chmodded.txt", "removed.txt", "gone.txt", "submodule"}

	before, err := conflictresolver.SnapshotFiles(dir, files)
	assert.NilError(t, err)
	assert.Assert(t, before["textual.txt"].HasMarkers)
	assert.Assert(t, !before["binary.bin"].HasMarkers)
	assert.Assert(t, before["binary.bin"].Exists)
	assert.Assert(t, !before["gone.txt"].Exists)
	assert.Assert(t, !before["chmodded.txt"].Executable)
	assert.Assert(t, before["submodule"].Exists)
	assert.Assert(t, before["submodule"].IsDir)
	assert.Assert(t, !before["submodule"].HasMarkers)
	assert.Equal(t, before["submodule"].Gitlink, "", "a plain directory has no gitlink commit")

	// Simulate a resolver: fixes the textual conflict, rewrites touched.txt,
	// deletes removed.txt, makes chmodded.txt executable (a mode-only
	// add/add conflict resolved by picking the executable side) and leaves
	// the rest alone.
	write("textual.txt", "ours and theirs\n")
	write("touched.txt", "resolved by the tool\n")
	assert.NilError(t, os.Remove(filepath.Join(dir, "removed.txt")))
	assert.NilError(t, os.Chmod(filepath.Join(dir, "chmodded.txt"), 0o755))

	// Editing inside the submodule's checkout is not a resolution of the
	// gitlink conflict, so the directory still counts as untouched.
	write("submodule/inner.txt", "edited by the tool\n")

	unverifiable, err := conflictresolver.UnverifiableFiles(dir, files, before)
	assert.NilError(t, err)
	assert.DeepEqual(t, unverifiable, []string{"binary.bin", "kept.txt", "gone.txt", "submodule"})

	// Markers left in place after the resolver touches the file are still
	// caught by the marker check, not by this one.
	remaining, err := conflictresolver.FilesWithConflictMarkers(dir, files)
	assert.NilError(t, err)
	assert.Equal(t, len(remaining), 0)

	// Files the snapshot doesn't know about are ignored rather than flagged.
	unverifiable, err = conflictresolver.UnverifiableFiles(dir, []string{"unknown.txt"}, before)
	assert.NilError(t, err)
	assert.Equal(t, len(unverifiable), 0)
}

func TestSnapshotFiles_Gitlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// "sub" is an initialised submodule: a nested repository with a commit
	// checked out. What `git add sub` stages is that commit.
	testutil.ExecOrFail(t, dir, `
		git init -q sub
		git -C sub config user.email test@example.com
		git -C sub config user.name "Test User"
		git -C sub config commit.gpgsign false
		echo s0 > sub/s.txt
		git -C sub add s.txt
		git -C sub commit -qm s0
	`)

	head := func() string {
		out, err := os.ReadFile(filepath.Join(dir, "sub", ".git", "HEAD"))
		assert.NilError(t, err)

		ref := strings.TrimSpace(strings.TrimPrefix(string(out), "ref: "))
		sha, err := os.ReadFile(filepath.Join(dir, "sub", ".git", ref))
		assert.NilError(t, err)

		return strings.TrimSpace(string(sha))
	}

	files := []string{"sub"}

	before, err := conflictresolver.SnapshotFiles(dir, files)
	assert.NilError(t, err)
	assert.Assert(t, before["sub"].IsDir)
	assert.Equal(t, before["sub"].Gitlink, head())

	// Editing files inside the submodule does not change what would be
	// staged, so the conflict is still unresolved.
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "sub", "s.txt"), []byte("edited\n"), 0o644))

	unverifiable, err := conflictresolver.UnverifiableFiles(dir, files, before)
	assert.NilError(t, err)
	assert.DeepEqual(t, unverifiable, []string{"sub"})

	// Checking out (here: creating) a different commit is a resolution.
	testutil.ExecOrFail(t, dir, `
		git -C sub commit -qam s1
	`)
	assert.Assert(t, head() != before["sub"].Gitlink)

	unverifiable, err = conflictresolver.UnverifiableFiles(dir, files, before)
	assert.NilError(t, err)
	assert.Equal(t, len(unverifiable), 0)

	// An uninitialised submodule is an empty directory: no gitlink, and it
	// must not pick up the enclosing repository's HEAD.
	testutil.ExecOrFail(t, dir, `
		git init -q .
		mkdir empty
	`)

	states, err := conflictresolver.SnapshotFiles(dir, []string{"empty"})
	assert.NilError(t, err)
	assert.Assert(t, states["empty"].IsDir)
	assert.Equal(t, states["empty"].Gitlink, "")
}

func TestSnapshotFiles_Symlinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "target.txt"), []byte("original\n"), 0o644))
	assert.NilError(t, os.Symlink("target.txt", filepath.Join(dir, "link")))

	files := []string{"link"}

	before, err := conflictresolver.SnapshotFiles(dir, files)
	assert.NilError(t, err)
	assert.Assert(t, before["link"].Exists)
	assert.Assert(t, before["link"].IsSymlink)
	assert.Assert(t, !before["link"].HasMarkers)

	// Editing the file the link points at is not a resolution of the link
	// conflict: git stages the link value, which is unchanged.
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "target.txt"), []byte("edited\n"), 0o644))

	unverifiable, err := conflictresolver.UnverifiableFiles(dir, files, before)
	assert.NilError(t, err)
	assert.DeepEqual(t, unverifiable, []string{"link"})

	// Re-pointing the link is a change.
	assert.NilError(t, os.Remove(filepath.Join(dir, "link")))
	assert.NilError(t, os.Symlink("other.txt", filepath.Join(dir, "link")))

	unverifiable, err = conflictresolver.UnverifiableFiles(dir, files, before)
	assert.NilError(t, err)
	assert.Equal(t, len(unverifiable), 0)

	// Replacing the link with a regular file of identical bytes is a change too.
	assert.NilError(t, os.Remove(filepath.Join(dir, "link")))
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "link"), []byte("target.txt"), 0o644))

	unverifiable, err = conflictresolver.UnverifiableFiles(dir, files, before)
	assert.NilError(t, err)
	assert.Equal(t, len(unverifiable), 0)
}
