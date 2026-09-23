// Package workyard creates and operates on "workyards": fast copies of a
// directory tree in which every git repository is replaced by a git worktree
// checked out at a chosen branch, so a whole multi-repo workspace can be
// duplicated in seconds and worked on in isolation.
//
// Like a git worktree, a workyard holds only a pointer back to its source (the
// .workyard file at its root); the source keeps the metadata for each of its
// workyards under .workyard/yards/, next to its optional config.yaml.
package workyard

import (
	"errors"
	"io"
	"time"
)

// MetadataVersion is the current format version of the metadata files.
const MetadataVersion = 1

// Repo describes one repository in a workyard.
type Repo struct {
	// Path is the repository's location relative to the yard root (and to the
	// source directory).
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

// Metadata describes one workyard. It is stored in the source directory at
// .workyard/yards/<id>.json.
type Metadata struct {
	Version    int       `json:"version"`
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	YasVersion string    `json:"yasVersion"`
	GitVersion string    `json:"gitVersion"`
	Source     string    `json:"source"`
	Target     string    `json:"target"`
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
	// Exec configures the exec command.
	Exec ExecConfig `yaml:"exec"`
}

// ExecConfig configures the exec command.
type ExecConfig struct {
	// Parallel runs the command in every repository at once with its output
	// captured, instead of one repository at a time connected to the
	// terminal. The --parallel and --no-parallel options override it.
	Parallel bool `yaml:"parallel"`
}

// RepoConfig holds per-repository configuration.
type RepoConfig struct {
	Trunk string `yaml:"trunk"`
}

// Yard is an existing workyard.
type Yard struct {
	// Root is the yard directory (the one holding the .workyard pointer).
	Root string
	// Source is the directory the yard was created from.
	Source string
	ID     string
	Meta   Metadata
}

// CreateOptions configures Create.
type CreateOptions struct {
	Source string
	Target string
	// Branch to check out in every repository; defaults to the basename of
	// Target.
	Branch string
	// Parallelism is the number of concurrent git operations (default: number
	// of CPUs); file copies run with four times as many.
	Parallelism int
	// Log receives warnings and informational messages; nil discards them.
	Log io.Writer
}

// RemoveOptions configures Yard.Remove.
type RemoveOptions struct {
	// Force is passed to git worktree remove: 1 removes worktrees with
	// uncommitted changes (and deletes created branches even when unmerged),
	// 2 also removes locked worktrees.
	Force int
	Log   io.Writer
}

// RunOptions configures Yard.Run.
type RunOptions struct {
	// Parallelism is the number of repositories to run in concurrently
	// (default: twice the number of CPUs).
	Parallelism int
	// Ordered delivers results in repository path order instead of completion
	// order.
	Ordered bool
}

// StdIO is the set of standard streams a command is connected to. A nil
// stream is connected to nothing (the command reads EOF or writes to /dev/null).
type StdIO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Result is the outcome of running a command in one repository.
type Result struct {
	Repo Repo
	// Branch is the branch checked out when the command ran ("HEAD" when
	// detached).
	Branch   string
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	// Err is set when the command could not be run at all (as opposed to
	// exiting non-zero), e.g. because the repository directory is missing or
	// the program was not found.
	Err error
}

var (
	ErrNotAWorkyard   = errors.New("not inside a workyard (hint: run from inside a workyard or set WORKYARD_ROOT)")
	ErrPartialFailure = errors.New("workyard creation failed")
	ErrTargetNotEmpty = errors.New("target directory exists and is not empty")
	ErrDirty          = errors.New("worktrees have uncommitted changes (hint: use --force to remove them anyway)")
	// ErrSourceIsRepo is returned when the source is, or is inside, a git
	// repository: the point of a workyard is a root that is not one.
	ErrSourceIsRepo = errors.New("Workyard cannot be a git repository. Create a git worktree instead") //nolint:staticcheck // user-facing sentence
	// ErrNestedWorkyard is returned when the source is itself a workyard.
	ErrNestedWorkyard = errors.New("source is inside a workyard")
	// ErrUnresolvedBranch is returned by Create when the branch cannot be
	// checked out in one or more repositories; nothing is created then.
	ErrUnresolvedBranch = errors.New("cannot resolve branch in some repositories")
)

// SourceMissingError is returned by Open when the yard's source directory no
// longer exists, so its metadata cannot be read. The yard can still be deleted
// with RemoveOrphan.
type SourceMissingError struct {
	Root   string
	Source string
}

func (e *SourceMissingError) Error() string {
	return "source directory " + e.Source + " of workyard " + e.Root + " no longer exists"
}
