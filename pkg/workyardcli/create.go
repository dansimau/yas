package workyardcli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/dansimau/yas/pkg/progress"
	"github.com/dansimau/yas/pkg/workyard"
)

const createLongHelp = `Creates the workyard <name>: a copy of the source directory tree in which
every git repository becomes a git worktree of the source repository, checked
out at the branch (default: the name). Everything else is cloned
(copy-on-write, on APFS) or copied.

The workyard is created at --dest if given, else in <yards-dir>/<name>, where
yards-dir is set in the source's .workyard/config.yaml (relative to the
source, "~" for your home directory; default: .workyard/yards). The name may
contain slashes, like a branch name. With --dest the name is optional: the
branch then defaults to the basename of the destination.

When the branch does not exist in a repository it is created from the
repository's trunk (main or master, or "trunk" in the source's
.workyard/config.yaml). A branch that exists only on a remote is created to
track it, and a tag or commit is checked out detached.

The source defaults to that of the workyard containing the current directory,
else the nearest ancestor of the current directory with a .workyard directory
(which the first workyard created from a source adds), else the current
directory itself.

The source must not be (or be inside) a git repository or another workyard.
Files ignored by git are not copied into worktrees, and submodules are not
initialised.`

type createCmd struct {
	Source string `description:"Directory to copy (default: the current workyard's source, see above)" long:"source"  short:"s"`
	Dest   string `description:"Directory to create (default: <yards-dir>/<name>)"                     long:"dest"    short:"d"`
	Branch string `description:"Branch to check out in every repository (default: the name)"           long:"branch"  short:"b"`
	DryRun bool   `description:"Show what would be done without creating anything"                     long:"dry-run"`

	Args struct {
		Name string `description:"Name of the workyard, and default branch" positional-arg-name:"name"`
	} `positional-args:"yes"`
}

func (c *createCmd) SkipYardCheck() bool {
	return true
}

func (c *createCmd) Execute(args []string) error {
	if len(args) > 0 {
		return NewUsageError("unexpected arguments: " + strings.Join(args, " "))
	}

	if c.Args.Name == "" && c.Dest == "" {
		return NewUsageError("no name given (hint: workyard create <name>)")
	}

	// Warnings are only shown in verbose mode; the default output is just the
	// status line.
	log := io.Discard
	if current.cmd.Verbose {
		log = os.Stderr
	}

	source := c.Source
	if source == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		source, err = workyard.FindSource(cwd)
		if err != nil {
			return WrapError(err, ExitUsage)
		}
	}

	opts := workyard.CreateOptions{
		Source:      source,
		Name:        c.Args.Name,
		Target:      c.Dest,
		Branch:      c.Branch,
		Parallelism: current.cmd.Parallelism,
		Log:         log,
	}

	ctx := context.Background()

	if c.DryRun {
		plan, err := workyard.PlanCreate(ctx, opts)
		if err != nil {
			return WrapError(err, ExitFailure)
		}

		plan.Describe(os.Stdout)

		if failed := plan.Failed(); len(failed) > 0 {
			return NewError(fmt.Sprintf("branch cannot be resolved in %d repositories", len(failed)))
		}

		return nil
	}

	spinner := progress.NewSpinner(os.Stderr, "Creating workyard")
	spinner.Start()

	created, err := workyard.Create(ctx, opts)
	if err != nil {
		spinner.Stop("")

		return WrapError(err, ExitFailure)
	}

	spinner.Stop(fmt.Sprintf("Created workyard in %s", created.Elapsed.Round(10*time.Millisecond)))

	if current.cmd.Verbose {
		printVerboseNotes(created.Yard)
	}

	return nil
}

// printVerboseNotes reports what a worktree does not carry over from its
// source: ignored files and uninitialised submodules.
func printVerboseNotes(yard *workyard.Yard) {
	for _, repo := range yard.Meta.Repos {
		if repo.Submodules > 0 {
			fmt.Fprintf(os.Stderr, "warning: %s has %d submodule(s), which were not initialised\n", repo.Path, repo.Submodules)
		}

		ignored, err := gitexec.WithRepo(repo.Source).IgnoredTopLevel()
		if err != nil || len(ignored) == 0 {
			continue
		}

		fmt.Fprintf(os.Stderr, "%s: ignored in source, not copied: %s\n", repo.Path, strings.Join(ignored, " "))
	}
}
