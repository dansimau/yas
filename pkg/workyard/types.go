// Package workyard creates and operates on "workyards": fast copies of a
// directory tree in which every git repository is replaced by a git worktree
// checked out at a chosen branch, so a whole multi-repo workspace can be
// duplicated in seconds and worked on in isolation.
package workyard

import (
	"errors"
	"io"
	"time"
)

// MetadataVersion is the current format version of .workyard/metadata.json.
const MetadataVersion = 1

// Repo describes one repository in a workyard.
type Repo struct {
	// Path is the repository's location relative to the yard root (and to the
	// source directory). "." when the source itself is a repository.
	Path string `json:"path"`
	// Source is the absolute path of the source repository the worktree was
	// created from.
	Source string `json:"source"`
	// CommonDir is the absolute path of the git directory shared by the source
	// repository and its worktrees.
	CommonDir string `json:"commonDir"`
	// Ref is the branch (or, when detached, the commit-ish) the worktree was
	// created at.
	Ref string `json:"ref"`
	// CreatedBranch is true when the branch was created by workyard rather
	// than existing beforehand.
	CreatedBranch bool `json:"createdBranch"`
	// Detached is true when the worktree has a detached HEAD.
	Detached bool `json:"detached"`
	// Submodules is the number of submodules in the worktree, which workyard
	// does not initialise.
	Submodules int `json:"submodules"`
}

// Metadata is the content of a workyard's .workyard/metadata.json.
type Metadata struct {
	Version    int       `json:"version"`
	CreatedAt  time.Time `json:"createdAt"`
	YasVersion string    `json:"yasVersion"`
	GitVersion string    `json:"gitVersion"`
	Source     string    `json:"source"`
	Branch     string    `json:"branch"`
	Complete   bool      `json:"complete"`
	Repos      []Repo    `json:"repos"`
}

// Config is the content of a source directory's optional
// .workyard/config.yaml.
type Config struct {
	Version int `yaml:"version"`
	// Trunk is the branch new branches are created from when the requested
	// branch does not exist in a repository. Autodetected (main, master) when
	// empty.
	Trunk string `yaml:"trunk"`
	// Repos holds per-repository overrides, keyed by path relative to the
	// source directory.
	Repos map[string]RepoConfig `yaml:"repos"`
}

// RepoConfig holds per-repository configuration.
type RepoConfig struct {
	Trunk string `yaml:"trunk"`
}

// Yard is an existing workyard.
type Yard struct {
	Root string
	Meta Metadata
}

// CopyMode selects how non-repository files are copied.
type CopyMode int

const (
	// CopyAuto clones subtrees where the filesystem supports it (APFS on
	// macOS) and falls back to a plain copy otherwise.
	CopyAuto CopyMode = iota
	// CopyClone requires cloning and fails when it is not possible.
	CopyClone
	// CopyPlain never clones.
	CopyPlain
)

// ParseCopyMode parses "auto", "clone" or "plain".
func ParseCopyMode(s string) (CopyMode, error) {
	switch s {
	case "", "auto":
		return CopyAuto, nil
	case "clone":
		return CopyClone, nil
	case "plain":
		return CopyPlain, nil
	}

	return CopyAuto, errors.New("invalid copy mode " + s + " (expected auto, clone or plain)")
}

// Runner executes named tasks concurrently and reports on them.
// *progress.Runner satisfies it.
type Runner interface {
	Add(name string, fn func() error)
	Start(printResults bool) error
}

// CreateOptions configures Create.
type CreateOptions struct {
	Source string
	Target string
	// Branch to check out in every repository; defaults to the basename of
	// Target.
	Branch string
	// Jobs is the number of concurrent file copy operations (default
	// NumCPU*4); GitJobs the number of concurrent git operations (default
	// NumCPU).
	Jobs     int
	GitJobs  int
	CopyMode CopyMode
	// Detach creates detached worktrees for branches that are already checked
	// out elsewhere instead of failing.
	Detach bool
	// KeepPartial leaves a partially created yard in place on failure instead
	// of rolling it back.
	KeepPartial bool
	// AllowNested allows the source to be inside an existing workyard.
	AllowNested bool
	// Log receives warnings and informational messages; nil discards them.
	Log io.Writer
	// NewRunner constructs the Runner used for the git phase; nil runs the
	// tasks silently.
	NewRunner func(maxGoroutines int, header string) Runner
}

// RemoveOptions configures Yard.Remove.
type RemoveOptions struct {
	// Force is passed to git worktree remove: 1 removes worktrees with
	// uncommitted changes, 2 also removes locked worktrees.
	Force int
	// DeleteBranch deletes the branches workyard created.
	DeleteBranch bool
	Log          io.Writer
}

// RunOptions configures Yard.Run.
type RunOptions struct {
	// Jobs is the number of repositories to run in concurrently (default
	// NumCPU*2).
	Jobs int
	// Color forces git to emit color.
	Color bool
	// Ordered delivers results in repository path order instead of completion
	// order.
	Ordered bool
}

// Result is the outcome of running a git command in one repository.
type Result struct {
	Repo Repo
	// Branch is the branch checked out when the command ran ("HEAD" when
	// detached).
	Branch   string
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	// Err is set when the command could not be run at all (as opposed to
	// exiting non-zero), e.g. because the repository directory is missing.
	Err error
}

var (
	ErrNotAWorkyard   = errors.New("not inside a workyard (hint: run from inside a workyard or set WORKYARD_ROOT)")
	ErrPartialFailure = errors.New("workyard creation failed")
	ErrTargetNotEmpty = errors.New("target directory exists and is not empty")
	ErrDirty          = errors.New("worktrees have uncommitted changes (hint: use --force to remove them anyway)")
)
