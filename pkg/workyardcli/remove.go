package workyardcli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dansimau/yas/pkg/cliutil"
	"github.com/dansimau/yas/pkg/workyard"
)

const removeLongHelp = `Removes every worktree of the workyard through its source repository (so git
forgets about them), then deletes the directory. Without --force nothing is
removed when any worktree has uncommitted changes or untracked files.

Without a path, the workyard containing the current directory is removed after
confirmation (or immediately with --yes).`

type removeCmd struct {
	Force        []bool `description:"Remove worktrees with uncommitted changes; give twice to also remove locked worktrees" long:"force"         short:"f"`
	DeleteBranch bool   `description:"Also delete the branches that were created by workyard create"                         long:"delete-branch"`
	Yes          bool   `description:"Do not ask for confirmation"                                                           long:"yes"           short:"y"`

	Args struct {
		Path string `description:"Workyard to remove (default: the one containing the current directory)" positional-arg-name:"path"`
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

	if c.Args.Path != "" {
		yard, err = workyard.Open(c.Args.Path)
	} else {
		yard, err = workyard.Find(cwd)
	}

	if err != nil {
		return WrapError(err, ExitUsage)
	}

	if inside, err := isInside(yard.Root, cwd); err != nil {
		return err
	} else if inside {
		return NewUsageError(fmt.Sprintf("current directory is inside %s (hint: cd out of the workyard first)", yard.Root))
	}

	if c.Args.Path == "" && !c.Yes {
		if !cliutil.Confirm(fmt.Sprintf("Remove workyard %s (%d repositories)? [y/N]", yard.Root, len(yard.Meta.Repos))) {
			return NewError("aborted")
		}
	}

	err = yard.Remove(context.Background(), workyard.RemoveOptions{
		Force:        len(c.Force),
		DeleteBranch: c.DeleteBranch,
		Log:          os.Stderr,
	})
	if err != nil {
		return WrapError(err, ExitFailure)
	}

	fmt.Fprintf(os.Stderr, "Removed workyard %s\n", yard.Root)

	return nil
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
