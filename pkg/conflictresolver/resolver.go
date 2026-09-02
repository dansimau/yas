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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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
	if markerSize <= 0 {
		markerSize = DefaultMarkerSize
	}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}

		return false, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	for scanner.Scan() {
		if isConflictMarker(scanner.Bytes(), markerSize) {
			return true, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return false, err
	}

	return false, nil
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

// MarkerSizeFunc returns the conflict marker length in effect for a file
// (relative to the rebase directory).
type MarkerSizeFunc func(file string) (int, error)

// FilesWithConflictMarkers returns the subset of files (relative to dir) that
// still contain conflict markers. markerSize may be nil, in which case
// DefaultMarkerSize is used for every file.
func FilesWithConflictMarkers(dir string, files []string, markerSize MarkerSizeFunc) ([]string, error) {
	var remaining []string

	for _, file := range files {
		size := DefaultMarkerSize

		if markerSize != nil {
			var err error

			size, err = markerSize(file)
			if err != nil {
				return nil, fmt.Errorf("failed to determine conflict marker size for %s: %w", file, err)
			}
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
