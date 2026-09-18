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

const createLongHelp = `Copies the source directory tree to the target. Every git repository found
along the way becomes a git worktree of the source repository, checked out at
the branch (default: the basename of the target). Everything else is cloned
(copy-on-write, on APFS) or copied.

When the branch does not exist in a repository it is created from the
repository's trunk (main or master, or "trunk" in the source's
.workyard/config.yaml). A branch that exists only on a remote is created to
track it, and a tag or commit is checked out detached.

The source must not be (or be inside) a git repository or another workyard.
Files ignored by git are not copied into worktrees, and submodules are not
initialised.`

type createCmd struct {
	Source string `default:"."                                                                             description:"Directory to copy" long:"source" short:"s"`
	Target string `description:"Directory to create (alternatively give it as the positional argument)"    long:"target"                   short:"t"`
	Branch string `description:"Branch to check out in every repository (default: basename of the target)" long:"branch"                   short:"b"`
	DryRun bool   `description:"Show what would be done without creating anything"                         long:"dry-run"`

	Args struct {
		Target string `description:"Directory to create" positional-arg-name:"target"`
	} `positional-args:"yes"`
}

func (c *createCmd) SkipYardCheck() bool {
	return true
}

func (c *createCmd) Execute(args []string) error {
	if len(args) > 0 {
		return NewUsageError("unexpected arguments: " + strings.Join(args, " "))
	}

	target := c.Target

	switch {
	case target != "" && c.Args.Target != "":
		return NewUsageError("target given both as --target and as an argument")
	case target == "" && c.Args.Target == "":
		return NewUsageError("no target given (hint: workyard create --branch <branch> <target>)")
	case target == "":
		target = c.Args.Target
	}

	// Warnings are only shown in verbose mode; the default output is just the
	// status line.
	log := io.Discard
	if current.cmd.Verbose {
		log = os.Stderr
	}

	opts := workyard.CreateOptions{
		Source:      c.Source,
		Target:      target,
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
