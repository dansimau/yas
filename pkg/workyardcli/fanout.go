package workyardcli

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/dansimau/yas/pkg/workyard"
	"golang.org/x/term"
)

var fanOutCommands = []string{"status", "diff", "exec"}

func isFanOutCommand(name string) bool {
	return slices.Contains(fanOutCommands, name)
}

const passthroughHelp = `
Options workyard does not recognise are passed to the command, as is
everything after the first non-option argument. Use "--" to pass an option
that workyard would otherwise interpret itself.`

const capturedHelp = `

The repositories run at once, with the command's output captured; the global
--parallelism and --unordered options control how. A header
"==> <path> (<branch>)" is printed for each repository that produced output.
The exit code is 1 when the command failed in any repository.`

const (
	statusLongHelp = `Runs "git status --short --branch" in every repository of the workyard.
Any arguments replace the default ones, e.g. "workyard st --long".` + passthroughHelp + capturedHelp
	diffLongHelp = `Runs "git diff <args>" in every repository of the workyard.` + passthroughHelp + capturedHelp
	execLongHelp = `Runs an arbitrary command in every repository of the workyard, with the
repository as the working directory, e.g. "workyard exec make test" or
"workyard exec git log --oneline -3". The command is run directly, not through
a shell; use "workyard exec sh -c '...'" for shell syntax.` + passthroughHelp + `

By default the repositories run one at a time, in path order, with the command
connected to the terminal so that it can be interactive (and color its output
as it would for you); a header "==> <path> (<branch>)" precedes each. With
--parallel the repositories run at once with the command's output captured;
then the global --parallelism and --unordered options apply and the header is
printed only for repositories that produced output. Set
"exec: {parallel: true}" in the source's .workyard/config.yaml to make
--parallel the default; --no-parallel overrides it. Either way the exit code is
1 when the command failed in any repository.`
)

type statusCmd struct{}

func (c *statusCmd) Execute(args []string) error {
	gitArgs := passthroughArgs(args)
	if len(gitArgs) == 0 {
		gitArgs = []string{"--short", "--branch"}
	}

	return fanOut(append([]string{"git", "status"}, gitArgs...))
}

type diffCmd struct{}

func (c *diffCmd) Execute(args []string) error {
	return fanOut(append([]string{"git", "diff"}, passthroughArgs(args)...))
}

type execCmd struct {
	Parallel   bool `description:"Run in every repository at once, capturing output"                                          long:"parallel"`
	NoParallel bool `description:"Run in one repository at a time, connected to the terminal (the default unless configured)" long:"no-parallel"`
}

func (c *execCmd) Execute(args []string) error {
	command := passthroughArgs(args)
	if len(command) == 0 {
		return NewUsageError("no command given (e.g. workyard exec git log --oneline -3)")
	}

	if c.Parallel && c.NoParallel {
		return NewUsageError("--parallel and --no-parallel cannot be combined")
	}

	cfg, err := workyard.LoadConfig(current.yard.Source)
	if err != nil {
		return err
	}

	parallel := cfg.Exec.Parallel
	if c.Parallel {
		parallel = true
	} else if c.NoParallel {
		parallel = false
	}

	if parallel {
		return fanOut(command)
	}

	return fanOutSerial(command)
}

// passthroughArgs combines the unrecognised options collected while parsing
// with the positional arguments, preserving the command line order (all
// collected options precede the first positional argument).
func passthroughArgs(positional []string) []string {
	return append(slices.Clone(current.passthrough), positional...)
}

// fanOut runs the command in every repository at once, capturing its output,
// and renders the results: a header per repository that produced output,
// then its output.
func fanOut(command []string) error {
	var failures failureCount

	err := current.yard.Run(context.Background(), workyard.RunOptions{
		Parallelism: current.cmd.Parallelism,
		Ordered:     !current.cmd.Unordered,
	}, command, func(r workyard.Result) {
		failures.record(r)

		if r.Err != nil || (len(r.Stdout) == 0 && len(r.Stderr) == 0) {
			return
		}

		printHeader(r.Repo, r.Branch)

		_, _ = os.Stdout.Write(r.Stdout)
		_, _ = os.Stderr.Write(r.Stderr)
	})
	if err != nil {
		return err
	}

	return failures.err(command)
}

// fanOutSerial runs the command in one repository at a time, connected to the
// terminal, with a header before each.
func fanOutSerial(command []string) error {
	var failures failureCount

	stdio := workyard.StdIO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}

	err := current.yard.RunSerial(context.Background(), stdio, command, printHeader, failures.record)
	if err != nil {
		return err
	}

	return failures.err(command)
}

func printHeader(repo workyard.Repo, branch string) {
	header := fmt.Sprintf("==> %s (%s)", repo.Path, branch)
	if term.IsTerminal(int(os.Stdout.Fd())) {
		header = "\x1b[1m" + header + "\x1b[0m"
	}

	fmt.Println(header)
}

// failureCount tallies the repositories in which a command failed or could
// not be run.
type failureCount int

func (c *failureCount) record(r workyard.Result) {
	if r.Err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s: %v\n", r.Repo.Path, r.Err)
	}

	if r.Err != nil || r.ExitCode != 0 {
		*c++
	}
}

func (c failureCount) err(command []string) error {
	if c == 0 {
		return nil
	}

	return NewError(fmt.Sprintf("%s failed in %d of %d repositories", command[0], int(c), len(current.yard.Meta.Repos)))
}
