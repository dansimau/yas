package workyardcli

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/dansimau/yas/pkg/workyard"
	"golang.org/x/term"
)

var fanOutCommands = []string{"status", "diff", "git"}

func isFanOutCommand(name string) bool {
	return slices.Contains(fanOutCommands, name)
}

const fanOutOptionsHelp = `
Options workyard does not recognise are passed to git, as is everything after
the first non-option argument. Use "--" to pass an option that workyard would
otherwise interpret itself. The global --jobs, --unordered, --header, --quiet
and --color options control how results are run and shown.

A header "==> <path> (<branch>)" is printed for each repository when stdout is
a terminal or --header is given. The exit code is 1 when git failed in any
repository.`

const (
	statusLongHelp = `Runs "git status --short --branch" in every repository of the workyard.
Any arguments replace the default ones, e.g. "workyard st --long".` + fanOutOptionsHelp
	diffLongHelp = `Runs "git diff <args>" in every repository of the workyard.` + fanOutOptionsHelp
	gitLongHelp  = `Runs "git <args>" in every repository of the workyard, e.g. "workyard git log --oneline -3".` + fanOutOptionsHelp
)

type statusCmd struct{}

func (c *statusCmd) Execute(args []string) error {
	gitArgs := passthroughArgs(args)
	if len(gitArgs) == 0 {
		gitArgs = []string{"--short", "--branch"}
	}

	return fanOut(append([]string{"status"}, gitArgs...))
}

type diffCmd struct{}

func (c *diffCmd) Execute(args []string) error {
	return fanOut(append([]string{"diff"}, passthroughArgs(args)...))
}

type gitCmd struct{}

func (c *gitCmd) Execute(args []string) error {
	gitArgs := passthroughArgs(args)
	if len(gitArgs) == 0 {
		return NewUsageError("no git command given (e.g. workyard git log --oneline -3)")
	}

	return fanOut(gitArgs)
}

// passthroughArgs combines the unrecognised options collected while parsing
// with the positional arguments, preserving the command line order (all
// collected options precede the first positional argument).
func passthroughArgs(positional []string) []string {
	return append(slices.Clone(current.passthrough), positional...)
}

// fanOut runs the git command in every repository and renders the results.
func fanOut(gitArgs []string) error {
	cmd := current.cmd
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))

	color := false

	switch cmd.Color {
	case "always":
		color = true
	case "never":
	default:
		color = isTTY && os.Getenv("NO_COLOR") == "" && !hasColorArg(gitArgs)
	}

	showHeader := isTTY || cmd.Header
	failed := 0
	skipped := 0

	err := current.yard.Run(context.Background(), workyard.RunOptions{
		Jobs:    cmd.Jobs,
		Color:   color,
		Ordered: !cmd.Unordered,
	}, gitArgs, func(r workyard.Result) {
		if r.Err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", r.Repo.Path, r.Err)

			skipped++

			return
		}

		if r.ExitCode != 0 {
			failed++
		}

		hasOutput := len(r.Stdout) > 0 || len(r.Stderr) > 0

		if showHeader && (hasOutput || !cmd.Quiet) {
			header := fmt.Sprintf("==> %s (%s)", r.Repo.Path, r.Branch)
			if isTTY {
				header = "\x1b[1m" + header + "\x1b[0m"
			}

			fmt.Println(header)
		}

		_, _ = os.Stdout.Write(r.Stdout)
		_, _ = os.Stderr.Write(r.Stderr)
	})
	if err != nil {
		return err
	}

	if failed > 0 {
		return NewError(fmt.Sprintf("git exited with an error in %d of %d repositories", failed, len(current.yard.Meta.Repos)-skipped))
	}

	return nil
}

func hasColorArg(args []string) bool {
	for _, arg := range args {
		if arg == "--no-color" || arg == "--color" || strings.HasPrefix(arg, "--color=") {
			return true
		}
	}

	return false
}
