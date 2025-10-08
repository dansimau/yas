package yascli

import (
	"errors"
	"fmt"
	"os"
	"path"

	"github.com/dansimau/yas/pkg/fsutil"
	"github.com/jessevdk/go-flags"
)

var cmd *Cmd

type Cmd struct {
	DryRun        bool   `long:"dry-run" description:"Don't make any changes, just show what will happen"`
	RepoDirectory string `long:"repo" short:"r" description:"Repo directory"`
	Verbose       bool   `long:"verbose" short:"v" description:"Verbose output"`
}

func mustAddCommand(f *flags.Command, err error) *flags.Command {
	if err != nil {
		panic(err)
	}
	return f
}

// Run executes the program with the specified arguments and returns the code
// the process should exit with.
func Run(args ...string) (exitCode int) {
	// Must recreate this global on each invocation to reset flag values
	// between invocations.
	cmd = &Cmd{}

	parser := flags.NewParser(cmd, flags.HelpFlag)

	parser.CommandHandler = func(command flags.Commander, args []string) error {
		// Apply defaults to cmd
		if cmd.RepoDirectory == "" {
			gitDir, err := fsutil.SearchParentsForPathFromCwd(".git")
			if err != nil {
				return NewError("cannot find repository (.git directory) (hint: specify --repo or run yas from inside repostory)")
			}

			repoDir := path.Dir(gitDir)
			cmd.RepoDirectory = repoDir
		}

		if cmd.Verbose {
			os.Setenv("YAS_VERBOSE", "1")
			os.Setenv("XEXEC_VERBOSE", "1")
		}

		// Run command
		return command.Execute(args)
	}

	mustAddCommand(parser.AddCommand("add", "Add/set parent of branch", "", &addCmd{}))
	mustAddCommand(parser.AddCommand("annotate", "Annotate PR with stack information", "", &annotateCmd{})).Hidden = true
	mustAddCommand(parser.AddCommand("config", "Manage repository-specific configuration", "", &configCmd{}))
	mustAddCommand(parser.AddCommand("init", "Set up initial configuration", "", &initCmd{}))
	mustAddCommand(parser.AddCommand("list", "List stacks", "", &listCmd{}))
	mustAddCommand(parser.AddCommand("nb", "Create new branch", "", &nbCmd{}))
	mustAddCommand(parser.AddCommand("submit", "Submit", "", &submitCmd{}))
	mustAddCommand(parser.AddCommand("restack", "Rebase all branches in the current stack", "", &restackCmd{}))
	mustAddCommand(parser.AddCommand("sync", "Sync", "", &syncCmd{}))

	_, err := parser.ParseArgs(args)
	if err != nil {
		// Handle --help, which is represented as an error by the flags package
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			fmt.Fprintf(os.Stderr, "%s\n", err)
			return 0
		}

		if errors.Is(err, &Error{}) {
			// Error, just exit with a message
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		} else {
			// unexpected error so print stack trace, if there is one
			fmt.Fprintf(os.Stderr, "ERROR: %+v\n", err)
		}

		return 1
	}

	return 0
}
