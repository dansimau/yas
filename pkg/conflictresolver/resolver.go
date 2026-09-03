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
	"crypto/sha256"
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

// DefaultMarkerSize is the length of git's conflict markers unless a path
// sets the conflict-marker-size attribute.
const DefaultMarkerSize = 7

// MaxMarkerSize is the largest conflict-marker-size yas honours. Git accepts
// absurd values (and silently falls back to normal markers for some of them);
// treating anything larger as the default avoids building huge marker strings.
const MaxMarkerSize = 256

// normaliseMarkerSize maps sizes git wouldn't sensibly use to the default.
func normaliseMarkerSize(size int) int {
	if size <= 0 || size > MaxMarkerSize {
		return DefaultMarkerSize
	}

	return size
}

// conflictMarkerChars are the characters git repeats to open each side of a
// conflict. "=" is deliberately excluded because a run of "=" also appears in
// ordinary content (e.g. setext headings in Markdown) and is never present in
// a conflict without one of these.
var conflictMarkerChars = []byte{'<', '>', '|'}

// HasConflictMarkers reports whether the file at path still contains git
// conflict markers of the given length (see DefaultMarkerSize and git's
// conflict-marker-size attribute). A file that no longer exists (deleted as
// part of the resolution) has no markers.
func HasConflictMarkers(path string, markerSize int) (bool, error) {
	markerSize = normaliseMarkerSize(markerSize)

	f, err := openRegularFile(path)
	if err != nil || f == nil {
		return false, err
	}
	defer f.Close()

	found := false

	err = scanLinePrefixes(f, func(line []byte) bool {
		found = isConflictMarker(line, markerSize)

		return !found
	})

	return found, err
}

// linePrefixSize is how much of each line scanLinePrefixes hands to its
// callback. Conflict markers sit at the very start of a line and are at most
// MaxMarkerSize long (plus a space), so this is plenty.
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

// isConflictMarker reports whether line is a git conflict marker: exactly
// markerSize repetitions of a marker character followed by end of line or a
// space.
func isConflictMarker(line []byte, markerSize int) bool {
	for _, ch := range conflictMarkerChars {
		prefix := bytes.Repeat([]byte{ch}, markerSize)
		if !bytes.HasPrefix(line, prefix) {
			continue
		}

		rest := line[len(prefix):]
		if len(rest) == 0 || rest[0] == ' ' {
			return true
		}
	}

	return false
}

// markerRun returns the length of the run of ch that line starts with, if the
// run is followed by end of line or a space (the shape of every git conflict
// marker), and 0 otherwise.
func markerRun(line []byte, ch byte) int {
	n := 0
	for n < len(line) && line[n] == ch {
		n++
	}

	if n == 0 || (n < len(line) && line[n] != ' ') {
		return 0
	}

	return n
}

// DetectMarkerSize scans the file at path for git conflict markers and returns
// the marker length actually present: a run of "<" opening a hunk with a run of
// ">" of the same length closing one. It returns 0 when the file holds no
// recognisable markers (including when it is missing or not a regular file).
//
// If several lengths qualify, preferred wins when it is one of them and the
// longest otherwise, since a long run is the least likely to be ordinary
// content. Runs longer than MaxMarkerSize are not considered markers.
//
// Reading the size off the file is more reliable than asking git for the
// conflict-marker-size attribute: when .gitattributes is itself conflicted,
// `git check-attr` parses the marker-riddled working-tree copy and can report
// a size other than the one git used when it wrote the file.
func DetectMarkerSize(path string, preferred int) (int, error) {
	f, err := openRegularFile(path)
	if err != nil || f == nil {
		return 0, err
	}
	defer f.Close()

	opens := map[int]bool{}
	closes := map[int]bool{}

	err = scanLinePrefixes(f, func(line []byte) bool {
		if n := markerRun(line, '<'); n > 0 && n <= MaxMarkerSize {
			opens[n] = true
		} else if n := markerRun(line, '>'); n > 0 && n <= MaxMarkerSize {
			closes[n] = true
		}

		return true
	})
	if err != nil {
		return 0, err
	}

	best := 0

	for n := range opens {
		if !closes[n] {
			continue
		}

		if n == preferred {
			return n, nil
		}

		if n > best {
			best = n
		}
	}

	return best, nil
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

// MarkerSizeFunc returns the conflict marker length in effect for a file
// (relative to the rebase directory).
type MarkerSizeFunc func(file string) (int, error)

// FilesWithConflictMarkers returns the subset of files (relative to dir) that
// still contain conflict markers, using the marker size recorded for each file
// in before (see SnapshotFiles). Files missing from before are checked with
// DefaultMarkerSize.
//
// The size is deliberately not looked up again: if .gitattributes was itself
// conflicted and the resolver changed conflict-marker-size, a fresh lookup
// would describe markers git never wrote, and the ones it did write could go
// unnoticed.
func FilesWithConflictMarkers(dir string, files []string, before map[string]FileState) ([]string, error) {
	var remaining []string

	for _, file := range files {
		size := DefaultMarkerSize
		if prev, ok := before[file]; ok && prev.MarkerSize > 0 {
			size = prev.MarkerSize
		}

		hasMarkers, err := HasConflictMarkers(filepath.Join(dir, file), size)
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
	// target, not the file it points to, so Sum covers the target string.
	IsSymlink bool
	// IsDir is true when the path is a directory, which is how a conflicted
	// submodule (gitlink) appears in the working tree. Its contents are not
	// hashed: git stages the commit checked out inside it, held in Gitlink.
	IsDir bool
	// Gitlink is the commit checked out in the submodule at this path (what
	// `git add` would stage), or empty when the directory is not an
	// initialised submodule.
	Gitlink string
	// Sum is the SHA-256 of the file contents, or of the link target for a
	// symlink (zero when Exists is false or IsDir is true).
	Sum [sha256.Size]byte
	// HasMarkers is true when the file contained textual conflict markers, i.e.
	// its resolution can later be verified by checking the markers are gone.
	HasMarkers bool
	// MarkerSize is the conflict marker length in effect for this file, as
	// determined before the resolver ran: read from the markers themselves
	// when the file has any, otherwise from the caller's lookup.
	MarkerSize int
}

// SnapshotFiles records the state of each file (relative to dir) so that a
// resolver's work can be verified afterwards with FilesWithConflictMarkers and
// UnverifiableFiles.
//
// The conflict marker size for each file is read from the markers git wrote
// (see DetectMarkerSize). markerSize, which may be nil, supplies the expected
// size per file (e.g. from the conflict-marker-size attribute); it breaks ties
// when a file could be read either way and is recorded for files without
// markers.
func SnapshotFiles(dir string, files []string, markerSize MarkerSizeFunc) (map[string]FileState, error) {
	states := make(map[string]FileState, len(files))

	for _, file := range files {
		size := DefaultMarkerSize

		if markerSize != nil {
			var err error

			size, err = markerSize(file)
			if err != nil {
				return nil, fmt.Errorf("failed to determine conflict marker size for %s: %w", file, err)
			}

			size = normaliseMarkerSize(size)
		}

		path := filepath.Join(dir, file)

		state, err := snapshotFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", file, err)
		}

		// Only regular files can carry markers; a symlink's or gitlink's
		// resolution is verified by its value changing instead.
		detected, err := DetectMarkerSize(path, size)
		if err != nil {
			return nil, fmt.Errorf("failed to check %s for conflict markers: %w", file, err)
		}

		if detected > 0 {
			state.HasMarkers = true
			state.MarkerSize = detected
		} else {
			state.MarkerSize = size
		}

		states[file] = state
	}

	return states, nil
}

// sameContent reports whether two snapshots describe the same working-tree
// entry (existence, type and bytes), ignoring the marker bookkeeping.
func (s FileState) sameContent(other FileState) bool {
	return s.Exists == other.Exists && s.IsSymlink == other.IsSymlink && s.IsDir == other.IsDir &&
		s.Gitlink == other.Gitlink && s.Sum == other.Sum
}

func snapshotFile(path string) (FileState, error) {
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
		state.Sum = sha256.Sum256([]byte(target))

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

	f, err := os.Open(path)
	if err != nil {
		return FileState{}, err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return FileState{}, err
	}

	copy(state.Sum[:], h.Sum(nil))

	return state, nil
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

		now, err := snapshotFile(filepath.Join(dir, file))
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", file, err)
		}

		if now.sameContent(prev) {
			unverifiable = append(unverifiable, file)
		}
	}

	return unverifiable, nil
}
