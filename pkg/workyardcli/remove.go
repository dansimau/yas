package workyardcli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dansimau/yas/pkg/cliutil"
	"github.com/dansimau/yas/pkg/workyard"
)

const removeLongHelp = `Removes every worktree of the workyard through its source repository (so git
forgets about them), deletes the directory, and deletes the branches that
"workyard create" created where they are fully merged. Without --force nothing
is removed when any worktree has uncommitted changes or untracked files; with
it, dirty worktrees and unmerged created branches are deleted too.

The workyard is given by name (as for "workyard create": <yards-dir>/<name>
of the source the current directory belongs to) or by path; an argument that
exists as a file or directory is a path. Without either, the workyard
containing the current directory is removed after confirmation (or
immediately with --yes).`

type removeCmd struct {
	Force []bool `description:"Remove worktrees with uncommitted changes and delete unmerged branches; give twice to also remove locked worktrees" long:"force" short:"f"`
	Yes   bool   `description:"Do not ask for confirmation"                                                                                        long:"yes"   short:"y"`

	Args struct {
		Yard string `description:"Name or path of the workyard to remove (default: the one containing the current directory)" positional-arg-name:"name|path"`
	} `positional-args:"yes"`
}

func (c *removeCmd) SkipYardCheck() bool {
	return true
}

func (c *removeCmd) Execute(args []string) error {
	if len(args) > 0 {
		return NewUsageError("unexpected arguments: " + strings.Join(args, " "))
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	var yard *workyard.Yard

	switch {
	case c.Args.Yard == "":
		yard, err = workyard.Find(cwd)
	case exists(c.Args.Yard):
		yard, err = workyard.Open(c.Args.Yard)
	default:
		yard, err = workyard.FindNamed(cwd, c.Args.Yard)
	}

	// A yard whose source is gone can only be deleted outright.
	missing := &workyard.SourceMissingError{}
	if errors.As(err, &missing) {
		return c.removeOrphan(cwd, missing)
	}

	if err != nil {
		return WrapError(err, ExitUsage)
	}

	if err := c.confirm(cwd, yard.Root, fmt.Sprintf("Remove workyard %s (%d repositories)? [y/N]", yard.Root, len(yard.Meta.Repos))); err != nil {
		return err
	}

	err = yard.Remove(context.Background(), workyard.RemoveOptions{
		Force: len(c.Force),
		Log:   os.Stderr,
	})
	if err != nil {
		return WrapError(err, ExitFailure)
	}

	fmt.Fprintf(os.Stderr, "Removed workyard %s\n", yard.Root)

	return nil
}

func (c *removeCmd) removeOrphan(cwd string, missing *workyard.SourceMissingError) error {
	fmt.Fprintf(os.Stderr, "warning: %v; its worktrees cannot be unregistered\n", missing)

	if err := c.confirm(cwd, missing.Root, fmt.Sprintf("Delete directory %s? [y/N]", missing.Root)); err != nil {
		return err
	}

	if err := workyard.RemoveOrphan(missing.Root); err != nil {
		return WrapError(err, ExitFailure)
	}

	fmt.Fprintf(os.Stderr, "Removed workyard %s\n", missing.Root)

	return nil
}

// confirm refuses to remove a yard the user is inside, and asks before
// removing a yard that was found rather than named.
func (c *removeCmd) confirm(cwd, root, prompt string) error {
	if inside, err := isInside(root, cwd); err != nil {
		return err
	} else if inside {
		return NewUsageError(fmt.Sprintf("current directory is inside %s (hint: cd out of the workyard first)", root))
	}

	if c.Args.Yard == "" && !c.Yes && !cliutil.Confirm(prompt) {
		return NewError("aborted")
	}

	return nil
}

func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

// isInside reports whether dir is root or inside it, comparing real paths.
func isInside(root, dir string) (bool, error) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false, err
	}

	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, err
	}

	rel, err := filepath.Rel(realRoot, realDir)
	if err != nil {
		return false, nil //nolint:nilerr // different volumes: not inside
	}

	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, "../")), nil
}
