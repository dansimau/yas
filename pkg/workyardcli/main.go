// Package workyardcli provides the command-line interface for the workyard
// tool.
package workyardcli

import (
	"errors"
	"fmt"
	"os"

	"github.com/dansimau/yas/pkg/workyard"
	"github.com/jessevdk/go-flags"
)

// Cmd holds the global options. --unordered only affects st, diff and
// exec --parallel, but lives here so it can be given before or after the
// command name without clashing with the options those commands pass through.
type Cmd struct {
	Verbose     bool `description:"Verbose output"                                                        long:"verbose"     short:"v"`
	Parallelism int  `description:"Number of operations to run in parallel (default: based on CPU count)" long:"parallelism" short:"p"`

	Unordered bool `description:"st/diff/exec --parallel: print results as they complete instead of in path order" long:"unordered"`
}

// state is the per-invocation state shared by the commands.
type state struct {
	cmd  *Cmd
	yard *workyard.Yard
	// passthrough collects options that go-flags did not recognise while
	// parsing a fan-out command; they are handed to the command verbatim.
	passthrough []string
}

var current *state

// SkipYardCheck is implemented by commands that do not run inside an
// existing workyard.
type SkipYardCheck interface {
	SkipYardCheck() bool
}

func mustAddCommand(c *flags.Command, err error) *flags.Command {
	if err != nil {
		panic(err)
	}

	return c
}

// Run executes the program with the given arguments and returns the exit code.
func Run(args ...string) int {
	current = &state{cmd: &Cmd{}}

	parser := flags.NewParser(current.cmd, flags.HelpFlag|flags.PassDoubleDash)
	parser.LongDescription = "Create fast copies of a directory tree in which every git repository " +
		"becomes a worktree on a chosen branch, and run commands across all of them."

	parser.CommandHandler = func(command flags.Commander, args []string) error {
		if current.cmd.Verbose {
			if err := os.Setenv("YAS_VERBOSE", "1"); err != nil {
				return err
			}

			if err := os.Setenv("XEXEC_VERBOSE", "1"); err != nil {
				return err
			}
		}

		skip := false
		if checker, ok := command.(SkipYardCheck); ok {
			skip = checker.SkipYardCheck()
		}

		if !skip {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			yard, err := workyard.Find(cwd)
			if err != nil {
				return WrapError(err, ExitUsage)
			}

			current.yard = yard
		}

		return command.Execute(args)
	}

	// Options that go-flags does not recognise are passed on to the command
	// when a fan-out command is active; anywhere else they are errors as usual.
	seen := map[int]bool{}

	parser.UnknownOptionHandler = func(option string, _ flags.SplitArgument, remaining []string) ([]string, error) {
		active := parser.Active
		if active == nil || !isFanOutCommand(active.Name) {
			return nil, &flags.Error{Type: flags.ErrUnknownFlag, Message: fmt.Sprintf("unknown flag `%s'", option)}
		}

		// The handler does not receive the raw token, but the parser consumes
		// arguments in order, so it is the one before the remaining ones.
		index := len(args) - len(remaining) - 1
		if index >= 0 && index < len(args) && !seen[index] {
			seen[index] = true
			current.passthrough = append(current.passthrough, args[index])
		}

		return remaining, nil
	}

	mustAddCommand(parser.AddCommand("create", "Create a workyard", createLongHelp, &createCmd{}))
	mustAddCommand(parser.AddCommand("status", "Run git status in every repository", statusLongHelp, &statusCmd{})).Aliases = []string{"st"}
	mustAddCommand(parser.AddCommand("diff", "Run git diff in every repository", diffLongHelp, &diffCmd{}))
	mustAddCommand(parser.AddCommand("exec", "Run a command in every repository", execLongHelp, &execCmd{}))
	mustAddCommand(parser.AddCommand("list", "List the workyards created from a source directory", listLongHelp, &listCmd{})).Aliases = []string{"ls"}
	mustAddCommand(parser.AddCommand("remove", "Remove a workyard and its worktrees", removeLongHelp, &removeCmd{})).Aliases = []string{"rm"}

	for _, name := range fanOutCommands {
		parser.Find(name).PassAfterNonOption = true
	}

	_, err := parser.ParseArgs(args)
	if err == nil {
		return ExitOK
	}

	flagsErr := &flags.Error{}
	if errors.As(err, &flagsErr) {
		if flagsErr.Type == flags.ErrHelp {
			fmt.Println(err)

			return ExitOK
		}

		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)

		return ExitUsage
	}

	cliErr := &Error{}
	if errors.As(err, &cliErr) {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)

		return cliErr.ExitCode()
	}

	fmt.Fprintf(os.Stderr, "ERROR: %+v\n", err)

	return ExitFailure
}
