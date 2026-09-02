package conflictresolver_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/conflictresolver"
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
		// "=======" on its own is legitimate content (e.g. a Markdown setext
		// heading underline) and must not count as a marker.
		{"setext-heading", "Title\n=======\nbody\n", false},
		// Marker-like text that isn't at the start of a line, or is longer
		// than the 7-character marker, is not a marker.
		{"marker-not-at-start", "x <<<<<<< y\n", false},
		{"eight-chars", "<<<<<<<< not a marker\n", false},
		{"no-trailing-newline", "a\n<<<<<<< HEAD", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := conflictresolver.HasConflictMarkers(write(tc.name, tc.content), conflictresolver.DefaultMarkerSize)
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}

	t.Run("missing-file", func(t *testing.T) {
		t.Parallel()

		got, err := conflictresolver.HasConflictMarkers(filepath.Join(dir, "does-not-exist"), conflictresolver.DefaultMarkerSize)
		assert.NilError(t, err)
		assert.Assert(t, !got)
	})

	t.Run("custom-marker-size", func(t *testing.T) {
		t.Parallel()

		// With conflict-marker-size=12 git writes 12-character markers; the
		// default 7-character run must not be treated as a marker and vice versa.
		twelve := write("twelve", "a\n<<<<<<<<<<<< HEAD\nb\n============\nc\n>>>>>>>>>>>> theirs\n")

		got, err := conflictresolver.HasConflictMarkers(twelve, 12)
		assert.NilError(t, err)
		assert.Assert(t, got, "12-char markers should be detected with size 12")

		got, err = conflictresolver.HasConflictMarkers(twelve, conflictresolver.DefaultMarkerSize)
		assert.NilError(t, err)
		assert.Assert(t, !got, "12-char markers are not 7-char markers")

		seven := write("seven", "<<<<<<< HEAD\n")

		got, err = conflictresolver.HasConflictMarkers(seven, 12)
		assert.NilError(t, err)
		assert.Assert(t, !got, "7-char markers are not 12-char markers")

		// A non-positive size falls back to the default.
		got, err = conflictresolver.HasConflictMarkers(seven, 0)
		assert.NilError(t, err)
		assert.Assert(t, got)
	})
}

func TestFilesWithConflictMarkers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "clean.txt"), []byte("ok\n"), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "bad.txt"), []byte("<<<<<<< HEAD\nx\n=======\ny\n>>>>>>> z\n"), 0o644))

	remaining, err := conflictresolver.FilesWithConflictMarkers(dir, []string{"clean.txt", "bad.txt", "deleted.txt"}, nil)
	assert.NilError(t, err)
	assert.DeepEqual(t, remaining, []string{"bad.txt"})

	// A per-file marker size is honoured: with size 12 for bad.txt its
	// 7-character markers no longer count.
	sizeFor := func(file string) (int, error) {
		if file == "bad.txt" {
			return 12, nil
		}

		return conflictresolver.DefaultMarkerSize, nil
	}

	remaining, err = conflictresolver.FilesWithConflictMarkers(dir, []string{"clean.txt", "bad.txt"}, sizeFor)
	assert.NilError(t, err)
	assert.Equal(t, len(remaining), 0)

	_, err = conflictresolver.FilesWithConflictMarkers(dir, []string{"bad.txt"}, func(string) (int, error) {
		return 0, errors.New("attr lookup failed")
	})
	assert.ErrorContains(t, err, "failed to determine conflict marker size for bad.txt")
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

	// GIT_* variables must not leak into the resolver, otherwise its git
	// commands could inspect a different repository than req.Dir.
	envScript := `#!/bin/sh
printf '%s|%s|%s' "$GIT_DIR" "$GIT_WORK_TREE" "$KEEP_ME" > "$0.env"
`
	assert.NilError(t, os.WriteFile(filepath.Join(binDir, "env-claude"), []byte(envScript), 0o755))

	// t.Setenv is incompatible with t.Parallel, so run in a subprocess-free
	// way by temporarily setting the variables ourselves.
	for _, kv := range [][2]string{{"GIT_DIR", "/elsewhere/.git"}, {"GIT_WORK_TREE", "/elsewhere"}, {"KEEP_ME", "kept"}} {
		prev, had := os.LookupEnv(kv[0])
		assert.NilError(t, os.Setenv(kv[0], kv[1]))

		defer func() {
			if had {
				_ = os.Setenv(kv[0], prev)
			} else {
				_ = os.Unsetenv(kv[0])
			}
		}()
	}

	e := &conflictresolver.Claude{Binary: filepath.Join(binDir, "env-claude")}
	assert.NilError(t, e.Resolve(conflictresolver.Request{Dir: workDir, Files: []string{"conflicted.txt"}}))

	env, err := os.ReadFile(filepath.Join(binDir, "env-claude.env"))
	assert.NilError(t, err)
	assert.Equal(t, string(env), "||kept", "GIT_* stripped, other variables preserved")

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
	write("removed.txt", "will be deleted by the resolver\n")
	// "gone.txt" never existed in the working tree (deleted side of a
	// modify/delete conflict that git resolved towards the deletion).

	files := []string{"textual.txt", "binary.bin", "kept.txt", "touched.txt", "removed.txt", "gone.txt"}

	before, err := conflictresolver.SnapshotFiles(dir, files, nil)
	assert.NilError(t, err)
	assert.Assert(t, before["textual.txt"].HasMarkers)
	assert.Assert(t, !before["binary.bin"].HasMarkers)
	assert.Assert(t, before["binary.bin"].Exists)
	assert.Assert(t, !before["gone.txt"].Exists)

	// Simulate a resolver: fixes the textual conflict, rewrites touched.txt,
	// deletes removed.txt, leaves the rest alone.
	write("textual.txt", "ours and theirs\n")
	write("touched.txt", "resolved by the tool\n")
	assert.NilError(t, os.Remove(filepath.Join(dir, "removed.txt")))

	unverifiable, err := conflictresolver.UnverifiableFiles(dir, files, before)
	assert.NilError(t, err)
	assert.DeepEqual(t, unverifiable, []string{"binary.bin", "kept.txt", "gone.txt"})

	// Files the snapshot doesn't know about are ignored rather than flagged.
	unverifiable, err = conflictresolver.UnverifiableFiles(dir, []string{"unknown.txt"}, before)
	assert.NilError(t, err)
	assert.Equal(t, len(unverifiable), 0)

	// Marker size lookups are honoured and their errors surfaced.
	_, err = conflictresolver.SnapshotFiles(dir, []string{"textual.txt"}, func(string) (int, error) {
		return 0, errors.New("attr lookup failed")
	})
	assert.ErrorContains(t, err, "failed to determine conflict marker size for textual.txt")
}

func TestSnapshotFiles_Symlinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "target.txt"), []byte("original\n"), 0o644))
	assert.NilError(t, os.Symlink("target.txt", filepath.Join(dir, "link")))

	files := []string{"link"}

	before, err := conflictresolver.SnapshotFiles(dir, files, nil)
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
