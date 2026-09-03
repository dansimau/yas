// Package conflictresolver provides pluggable tools for automatically
// resolving merge conflicts that occur during a rebase.
//
// Resolvers are registered by name (e.g. "claude") and selected via yas
// configuration or command-line flags. Adding a new tool means implementing
// the Resolver interface and calling Register from an init function.
package conflictresolver

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/dansimau/yas/pkg/xexec"
)

// None is the resolver name meaning "do not attempt automatic resolution".
const None = "none"

// Request describes a set of conflicts for a resolver to fix.
type Request struct {
	// Dir is the working directory of the rebase (the worktree containing the
	// conflicted files). Resolvers must run in this directory.
	Dir string
	// Files are the paths (relative to Dir) of the files with unresolved
	// conflicts.
	Files []string
	// Branch is the branch being rebased.
	Branch string
	// Onto is the branch that Branch is being rebased onto.
	Onto string
	// CommitSubject is the subject line of the commit currently being
	// replayed, if known.
	CommitSubject string
}

// Resolver is an external tool that can resolve conflicts in place.
type Resolver interface {
	// Name is the identifier used in configuration and flags.
	Name() string
	// CheckAvailable returns an error if the resolver cannot run in this
	// environment (for example, its binary is not installed).
	CheckAvailable() error
	// Resolve edits the conflicted files in place. It should return an error
	// if the tool itself failed; leftover conflict markers are detected by
	// the caller.
	Resolve(req Request) error
}

var (
	registryMu sync.RWMutex
	registry   = map[string]func() Resolver{}
)

// Register makes a resolver available under the given name. It panics if the
// name is already taken or is the reserved name "none".
func Register(name string, factory func() Resolver) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if name == None {
		panic(fmt.Sprintf("conflictresolver: %q is a reserved name", name))
	}

	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("conflictresolver: %q is already registered", name))
	}

	registry[name] = factory
}

// New returns the resolver registered under name. It returns an error for
// unknown names, including "none" (callers should handle "none" themselves).
func New(name string) (Resolver, error) {
	registryMu.RLock()

	factory, ok := registry[name]

	registryMu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown conflict resolver %q (valid values: %s)", name, strings.Join(Names(), ", "))
	}

	return factory(), nil
}

// IsValid reports whether name is "none" or a registered resolver.
func IsValid(name string) bool {
	if name == None {
		return true
	}

	registryMu.RLock()
	defer registryMu.RUnlock()

	_, ok := registry[name]

	return ok
}

// Names returns all valid resolver names, including "none", sorted.
func Names() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := make([]string, 0, len(registry)+1)
	names = append(names, None)

	for name := range registry {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// MinMarkerSize is the shortest run of marker characters treated as a
// conflict marker. It is git's default conflict-marker-size. The attribute
// can make markers longer, so any run of at least this length counts; shorter
// runs are far more likely to be ordinary content (Markdown quotes, Python
// prompts) than a deliberately tiny marker.
const MinMarkerSize = 7

// conflictMarkerChars are the characters git repeats to open each side of a
// conflict. "=" is deliberately excluded because a run of "=" also appears in
// ordinary content (e.g. setext headings in Markdown) and is never present in
// a conflict without one of these.
var conflictMarkerChars = []byte{'<', '>', '|'}

// HasConflictMarkers reports whether the file at path contains git conflict
// markers of any length (see isConflictMarker). A missing path, or one that is
// not a regular file, has no markers.
func HasConflictMarkers(path string) (bool, error) {
	f, err := openRegularFile(path)
	if err != nil || f == nil {
		return false, err
	}
	defer f.Close()

	found := false

	err = scanLinePrefixes(f, func(line []byte) bool {
		found = isConflictMarker(line)

		return !found
	})

	return found, err
}

// isConflictMarker reports whether line is a git conflict marker: a run of at
// least MinMarkerSize repetitions of one marker character, followed by end of
// line or a space. The run may be any length, so markers written with a custom
// conflict-marker-size are recognised without consulting .gitattributes.
func isConflictMarker(line []byte) bool {
	if len(line) < MinMarkerSize || bytes.IndexByte(conflictMarkerChars, line[0]) < 0 {
		return false
	}

	n := 1
	for n < len(line) && line[n] == line[0] {
		n++
	}

	return n >= MinMarkerSize && (n == len(line) || line[n] == ' ')
}

// linePrefixSize is how much of each line scanLinePrefixes hands to its
// callback. Conflict markers sit at the very start of a line, so nothing past
// this can change whether a line is one.
const linePrefixSize = 64 * 1024

// scanLinePrefixes calls fn with the start of every line in r (without the
// trailing newline, and at most linePrefixSize bytes) until fn returns false
// or the input ends. Unlike bufio.Scanner it imposes no limit on line length:
// files with enormous lines, or binaries with no newline at all, are simply
// streamed past.
func scanLinePrefixes(r io.Reader, fn func(prefix []byte) bool) error {
	br := bufio.NewReaderSize(r, linePrefixSize)

	for {
		line, err := br.ReadSlice('\n')

		if len(line) > 0 && !fn(bytes.TrimSuffix(line, []byte{'\n'})) {
			return nil
		}

		// The line was longer than the buffer: discard the rest of it.
		for errors.Is(err, bufio.ErrBufferFull) {
			_, err = br.ReadSlice('\n')
		}

		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return err
		}
	}
}

// openRegularFile opens path for reading if it is a regular file. It returns a
// nil file (and nil error) when the path is missing or is not a regular file,
// e.g. a directory standing in for a conflicted submodule gitlink, or a
// symlink; neither can hold conflict markers.
func openRegularFile(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, err
	}

	if !info.Mode().IsRegular() {
		return nil, nil
	}

	return os.Open(path)
}

// FilesWithConflictMarkers returns the subset of files (relative to dir) that
// still contain conflict markers.
func FilesWithConflictMarkers(dir string, files []string) ([]string, error) {
	var remaining []string

	for _, file := range files {
		hasMarkers, err := HasConflictMarkers(filepath.Join(dir, file))
		if err != nil {
			return nil, fmt.Errorf("failed to check %s for conflict markers: %w", file, err)
		}

		if hasMarkers {
			remaining = append(remaining, file)
		}
	}

	return remaining, nil
}

// FileState captures a conflicted file as git left it, before a resolver runs.
type FileState struct {
	// Exists is false when git left no working-tree file (e.g. one side of a
	// modify/delete conflict).
	Exists bool
	// IsSymlink is true when the path is a symbolic link. Git stages the link
	// value, not the file it points to, so LinkTarget is what is compared.
	IsSymlink bool
	// LinkTarget is the link value when IsSymlink is true.
	LinkTarget string
	// IsDir is true when the path is a directory, which is how a conflicted
	// submodule (gitlink) appears in the working tree. Its contents are not
	// hashed: git stages the commit checked out inside it, held in Gitlink.
	IsDir bool
	// Gitlink is the commit checked out in the submodule at this path (what
	// `git add` would stage), or empty when the directory is not an
	// initialised submodule.
	Gitlink string
	// Executable is true when a regular file has the owner execute bit set,
	// which is the only mode bit git records (100755 vs 100644). A conflict
	// can be about the mode alone (both sides identical except one is
	// executable), so a change here counts as a resolution.
	Executable bool
	// Blob is the id of the blob git would stage for a regular file, i.e.
	// its contents after clean filters and line-ending conversion. Comparing
	// this rather than raw bytes means a change git would normalise away
	// (say LF to CRLF under core.autocrlf) is not mistaken for a resolution.
	Blob string
	// HasMarkers is true when the file contained textual conflict markers, i.e.
	// its resolution can later be verified by checking the markers are gone.
	HasMarkers bool
}

// SnapshotFiles records the state of each file (relative to dir) so that a
// resolver's work can be verified afterwards with FilesWithConflictMarkers and
// UnverifiableFiles.
func SnapshotFiles(dir string, files []string) (map[string]FileState, error) {
	states := make(map[string]FileState, len(files))

	for _, file := range files {
		path := filepath.Join(dir, file)

		state, err := snapshotFile(dir, file)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", file, err)
		}

		// Only regular files can carry markers; a symlink's or gitlink's
		// resolution is verified by its value changing instead.
		state.HasMarkers, err = HasConflictMarkers(path)
		if err != nil {
			return nil, fmt.Errorf("failed to check %s for conflict markers: %w", file, err)
		}

		states[file] = state
	}

	return states, nil
}

// sameContent reports whether two snapshots describe the same entry as git
// would stage it (existence, type, executable bit and blob), ignoring the
// marker bookkeeping.
func (s FileState) sameContent(other FileState) bool {
	return s.Exists == other.Exists && s.IsSymlink == other.IsSymlink && s.IsDir == other.IsDir &&
		s.LinkTarget == other.LinkTarget && s.Gitlink == other.Gitlink &&
		s.Executable == other.Executable && s.Blob == other.Blob
}

// snapshotFile describes file (relative to dir) the way `git add` would see
// it.
func snapshotFile(dir, file string) (FileState, error) {
	path := filepath.Join(dir, file)

	// Lstat so a symlink is described by its own link value rather than by
	// whatever it currently points at.
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return FileState{}, nil
		}

		return FileState{}, err
	}

	state := FileState{Exists: true}

	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return FileState{}, err
		}

		state.IsSymlink = true
		state.LinkTarget = target

		return state, nil
	}

	if info.IsDir() {
		// A conflicted submodule: git stages the commit checked out inside
		// it, not the directory's contents, so record that instead of a hash.
		state.IsDir = true

		commit, err := gitlinkCommit(path)
		if err != nil {
			return FileState{}, err
		}

		state.Gitlink = commit

		return state, nil
	}

	state.Executable = info.Mode()&0o100 != 0

	state.Blob, err = blobID(dir, file)
	if err != nil {
		return FileState{}, err
	}

	return state, nil
}

// blobID returns the id of the blob `git add` would create for file (relative
// to dir): its contents after the clean filters and end-of-line conversion
// configured for that path. Outside a repository no filters apply and the raw
// bytes are hashed.
func blobID(dir, file string) (string, error) {
	out, err := xexec.Command("git", "hash-object", "--", file).
		WithWorkingDir(dir).
		WithEnvVars(gitexec.CleanedGitEnv()).
		WithStdout(nil).
		Output()
	if err != nil {
		return "", fmt.Errorf("git hash-object failed: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// gitlinkCommit returns the commit checked out in the submodule at dir, i.e.
// what `git add dir` would record in the superproject. It returns "" when dir
// is not an initialised submodule (no .git entry) or has no commit yet.
func gitlinkCommit(dir string) (string, error) {
	// Without this guard git would walk up and report the superproject's HEAD
	// for an empty (uninitialised) submodule directory.
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}

		return "", err
	}

	out, err := xexec.Command("git", "rev-parse", "--verify", "--quiet", "HEAD").
		WithWorkingDir(dir).
		WithEnvVars(gitexec.CleanedGitEnv()).
		WithStdout(nil).
		WithStderr(nil).
		Output()
	if err != nil {
		// Unborn HEAD or a broken checkout: nothing git could stage.
		return "", nil
	}

	return strings.TrimSpace(string(out)), nil
}

// UnverifiableFiles returns the files whose resolution cannot be confirmed:
// git left them without textual conflict markers (binary content, a
// modify/delete or add/add conflict, a symlink or a submodule) and the
// resolver did not change or delete them. Such files would otherwise be staged exactly as git left them,
// silently taking one side of the conflict.
func UnverifiableFiles(dir string, files []string, before map[string]FileState) ([]string, error) {
	var unverifiable []string

	for _, file := range files {
		prev, ok := before[file]
		if !ok || prev.HasMarkers {
			// Marker-bearing files are verified by FilesWithConflictMarkers.
			continue
		}

		now, err := snapshotFile(dir, file)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", file, err)
		}

		if now.sameContent(prev) {
			unverifiable = append(unverifiable, file)
		}
	}

	return unverifiable, nil
}
