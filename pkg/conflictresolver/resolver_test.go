package conflictresolver_test

import (
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
}

func TestFilesWithConflictMarkers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "clean.txt"), []byte("ok\n"), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "bad.txt"), []byte("<<<<<<< HEAD\nx\n=======\ny\n>>>>>>> z\n"), 0o644))

	remaining, err := conflictresolver.FilesWithConflictMarkers(dir, []string{"clean.txt", "bad.txt", "deleted.txt"})
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
	for _, want := range []string{"`feature`", "`main`", `"Add thing"`, "- a.go", "- b.go", "Do NOT run git add"} {
		assert.Assert(t, strings.Contains(prompt, want), "prompt should contain %q:\n%s", want, prompt)
	}

	// Without a commit subject the line is omitted entirely.
	req.CommitSubject = ""
	assert.Assert(t, !strings.Contains(conflictresolver.Prompt(req), "currently being replayed"))
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
