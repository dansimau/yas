package workyardcli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/dansimau/yas/pkg/progress"
	"github.com/dansimau/yas/pkg/workyard"
	"golang.org/x/term"
)

const createLongHelp = `Copies the source directory tree to the target. Every git repository found
along the way becomes a git worktree of the source repository, checked out at
the branch (default: the basename of the target). Everything else is cloned
(copy-on-write, on APFS) or copied.

When the branch does not exist in a repository it is created from the
repository's trunk (main or master, or "trunk" in the source's
.workyard/config.yaml). A branch that exists only on a remote is created to
track it, and a tag or commit is checked out detached.

Files ignored by git are not copied into worktrees, and submodules are not
initialised.`

type createCmd struct {
	Source      string `default:"."                                                                                             description:"Directory to copy" long:"source"  short:"s"`
	Target      string `description:"Directory to create (alternatively give it as the positional argument)"                    long:"target"                   short:"t"`
	Branch      string `description:"Branch to check out in every repository (default: basename of the target)"                 long:"branch"                   short:"b"`
	GitJobs     int    `description:"Number of concurrent git operations (default: number of CPUs)"                             long:"git-jobs"`
	CopyMode    string `choice:"auto"                                                                                           choice:"clone"                  choice:"plain" default:"auto" description:"Whether to clone (copy-on-write) or copy files" env:"WORKYARD_COPY_MODE" long:"copy-mode"`
	Detach      bool   `description:"Detach instead of failing when the branch is already checked out in the source repository" long:"detach"`
	DryRun      bool   `description:"Show what would be done without creating anything"                                         long:"dry-run"`
	KeepPartial bool   `description:"Keep a partially created workyard on failure instead of removing it"                       long:"keep-partial"`
	AllowNested bool   `description:"Allow the source to be inside an existing workyard"                                        long:"allow-nested"`

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

	copyMode, err := workyard.ParseCopyMode(c.CopyMode)
	if err != nil {
		return NewUsageError(err.Error())
	}

	opts := workyard.CreateOptions{
		Source:      c.Source,
		Target:      target,
		Branch:      c.Branch,
		Jobs:        current.cmd.Jobs,
		GitJobs:     c.GitJobs,
		CopyMode:    copyMode,
		Detach:      c.Detach,
		KeepPartial: c.KeepPartial,
		AllowNested: c.AllowNested,
		Log:         os.Stderr,
		NewRunner:   newRunner,
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

	created, err := workyard.Create(ctx, opts)
	if err != nil {
		return WrapError(err, ExitFailure)
	}

	printCreateFooter(created)

	return nil
}

func printCreateFooter(created *workyard.Created) {
	yard := created.Yard

	fmt.Fprintf(os.Stderr, "Created workyard at %s: %d repositories, %d cloned, %d copied, %d skipped, in %s\n",
		yard.Root, len(yard.Meta.Repos), created.Cloned, created.Copied, created.Skipped, created.Elapsed.Round(1e7))
	fmt.Fprintln(os.Stderr, "note: files ignored by git are not copied into worktrees")

	for _, repo := range yard.Meta.Repos {
		if repo.Submodules > 0 {
			fmt.Fprintf(os.Stderr, "warning: %s has %d submodule(s), which were not initialised\n", repo.Path, repo.Submodules)
		}

		if !current.cmd.Verbose {
			continue
		}

		ignored, err := gitexec.WithRepo(repo.Source).IgnoredTopLevel()
		if err != nil || len(ignored) == 0 {
			continue
		}

		fmt.Fprintf(os.Stderr, "%s: ignored in source, not copied: %s\n", repo.Path, strings.Join(ignored, " "))
	}
}

// newRunner shows per-repository progress on a terminal and plain lines
// otherwise.
func newRunner(maxGoroutines int, header string) workyard.Runner {
	if term.IsTerminal(int(os.Stdout.Fd())) {
		return progress.New(maxGoroutines, header)
	}

	return &lineRunner{Runner: workyard.NewSilentRunner(maxGoroutines)}
}

// lineRunner prints one line per task to stderr as it completes.
type lineRunner struct {
	workyard.Runner
}

func (r *lineRunner) Add(name string, fn func() error) {
	r.Runner.Add(name, func() error {
		err := fn()
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ %s: %s\n", name, errorDetail(err))
		} else {
			fmt.Fprintf(os.Stderr, "✓ %s\n", name)
		}

		return err
	})
}

// errorDetail includes git's stderr in the message when available.
func errorDetail(err error) string {
	exitErr := &exec.ExitError{}
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return strings.TrimSpace(string(exitErr.Stderr))
	}

	return err.Error()
}
