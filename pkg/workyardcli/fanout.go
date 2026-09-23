package workyardcli

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/dansimau/yas/pkg/workyard"
	"golang.org/x/term"
)

var fanOutCommands = []string{"git", "exec"}

func isFanOutCommand(name string) bool {
	return slices.Contains(fanOutCommands, name)
}

const passthroughHelp = `
Options workyard does not recognise are passed to the command, as is
everything after the first non-option argument. Use "--" to pass an option
that workyard would otherwise interpret itself.`

const modesHelp = `

By default the repositories run one at a time, in path order, with the command
connected to the terminal so that it can be interactive (and color its output
as it would for you); a header "==> <path> (<branch>)" precedes each. With
--parallel the repositories run at once with the command's output captured;
then the global --parallelism and --unordered options apply and the header is
printed only for repositories that produced output. Set
"exec: {parallel: true}" in the source's .workyard/config.yaml to make
--parallel the default; --no-parallel overrides it. Either way the exit code is
1 when the command failed in any repository.`

const (
	gitLongHelp = `Runs git with the given arguments in every repository of the workyard, e.g.
"workyard git status" or "workyard git log --oneline -3". It is shorthand for
"workyard exec git ...": the arguments are passed to git verbatim.` + passthroughHelp + modesHelp
	execLongHelp = `Runs an arbitrary command in every repository of the workyard, with the
repository as the working directory, e.g. "workyard exec make test" or
"workyard exec git log --oneline -3". The command is run directly, not through
a shell; use "workyard exec sh -c '...'" for shell syntax.` + passthroughHelp + modesHelp
)

// modeFlags are the options of the commands that can run serially or in
// parallel.
type modeFlags struct {
	Parallel   bool `description:"Run in every repository at once, capturing output"                                          long:"parallel"`
	NoParallel bool `description:"Run in one repository at a time, connected to the terminal (the default unless configured)" long:"no-parallel"`
}

// run runs the command in every repository in the mode chosen by the flags,
// falling back to the source's configuration.
func (m modeFlags) run(command []string) error {
	if m.Parallel && m.NoParallel {
		return NewUsageError("--parallel and --no-parallel cannot be combined")
	}

	cfg, err := workyard.LoadConfig(current.yard.Source)
	if err != nil {
		return err
	}

	parallel := cfg.Exec.Parallel
	if m.Parallel {
		parallel = true
	} else if m.NoParallel {
		parallel = false
	}

	if parallel {
		return fanOut(command)
	}

	return fanOutSerial(command)
}

type gitCmd struct {
	modeFlags
}

func (c *gitCmd) Execute(args []string) error {
	return c.run(append([]string{"git"}, passthroughArgs(args)...))
}

type execCmd struct {
	modeFlags
}

func (c *execCmd) Execute(args []string) error {
	command := passthroughArgs(args)
	if len(command) == 0 {
		return NewUsageError("no command given (e.g. workyard exec git log --oneline -3)")
	}

	return c.run(command)
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
	var (
		failures failureCount
		headers  headers
	)

	err := current.yard.Run(context.Background(), workyard.RunOptions{
		Parallelism: current.cmd.Parallelism,
		Ordered:     !current.cmd.Unordered,
	}, command, func(r workyard.Result) {
		failures.record(r)

		if r.Err != nil || (len(r.Stdout) == 0 && len(r.Stderr) == 0) {
			return
		}

		headers.print(r.Repo, r.Branch)

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
	var (
		failures failureCount
		headers  headers
	)

	stdio := workyard.StdIO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}

	err := current.yard.RunSerial(context.Background(), stdio, command, headers.print, failures.record)
	if err != nil {
		return err
	}

	return failures.err(command)
}

// headers prints the "==> <path> (<branch>)" header before a repository's
// output, separated from the previous repository's output by a blank line.
type headers struct {
	printed bool
}

func (h *headers) print(repo workyard.Repo, branch string) {
	if h.printed {
		fmt.Println()
	}

	h.printed = true

	header := fmt.Sprintf("==> %s (%s)", repo.Path, branch)
	if term.IsTerminal(int(os.Stdout.Fd())) {
		header = "\x1b[1;36m" + header + "\x1b[0m"
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
