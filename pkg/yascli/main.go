// Package yascli provides the command-line interface for the yas tool.
package yascli

import (
	"errors"
	"fmt"
	"os"
	"path"

	"github.com/dansimau/yas/pkg/fsutil"
	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/jessevdk/go-flags"
)

var cmd *Cmd

type Cmd struct {
	DryRun        bool   `description:"Don't make any changes, just show what will happen" long:"dry-run"`
	RepoDirectory string `description:"Repo directory"                                     long:"repo"    short:"r"`
	Verbose       bool   `description:"Verbose output"                                     long:"verbose" short:"v"`
}

func mustAddCommand(f *flags.Command, err error) *flags.Command {
	if err != nil {
		panic(err)
	}

	return f
}

// SkipRepoCheck is an interface that commands can implement to skip the
// repository check in the CommandHandler.
type SkipRepoCheck interface {
	SkipRepoCheck() bool
}

// Run executes the program with the specified arguments and returns the code
// the process should exit with.
func Run(args ...string) (exitCode int) {
	// Must recreate this global on each invocation to reset flag values
	// between invocations.
	cmd = &Cmd{}

	parser := flags.NewParser(cmd, flags.HelpFlag)

	parser.CommandHandler = func(command flags.Commander, args []string) error {
		// Check if this command needs a repo check
		skipRepo := false
		if checker, ok := command.(SkipRepoCheck); ok {
			skipRepo = checker.SkipRepoCheck()
		}

		// Apply defaults to cmd
		if !skipRepo && cmd.RepoDirectory == "" {
			gitDir, err := fsutil.SearchParentsForPathFromCwd(".git")
			if err != nil {
				return NewError("cannot find repository (.git directory) (hint: specify --repo or run yas from inside repostory)")
			}

			repoDir := path.Dir(gitDir)

			// If we're in a worktree, resolve to the primary repo directory
			// to ensure config and state files are found correctly
			repo := gitexec.WithRepo(repoDir)
			if isWorktree, err := repo.IsLinkedWorktree(); err == nil && isWorktree {
				if primaryPath, err := repo.PrimaryWorktreePath(); err == nil {
					repoDir = primaryPath
				}
			}

			cmd.RepoDirectory = repoDir
		}

		if cmd.Verbose {
			if err := os.Setenv("YAS_VERBOSE", "1"); err != nil {
				return NewError("failed to set YAS_VERBOSE environment variable")
			}

			if err := os.Setenv("XEXEC_VERBOSE", "1"); err != nil {
				return NewError("failed to set XEXEC_VERBOSE environment variable")
			}
		}

		// Run command
		return command.Execute(args)
	}

	mustAddCommand(parser.AddCommand("abort", "Abort a restack operation in progress", "", &abortCmd{}))
	mustAddCommand(parser.AddCommand("add", "Add/set parent of branch", "", &addCmd{}))
	mustAddCommand(parser.AddCommand("annotate", "Annotate PR with stack information", "", &annotateCmd{})).Hidden = true
	mustAddCommand(parser.AddCommand("branch", "Work with branches", branchCmdLongHelp, &branchCmd{})).Aliases = []string{"nb", "br"}
	mustAddCommand(parser.AddCommand("config", "Manage repository-specific configuration", "", &configCmd{}))
	mustAddCommand(parser.AddCommand("continue", "Continue a restack operation after fixing conflicts", "", &continueCmd{}))
	mustAddCommand(parser.AddCommand("hook", "Print shell integration hook for bash or zsh", "", &hookCmd{}))
	mustAddCommand(parser.AddCommand("init", "Set up initial configuration", "", &initCmd{}))
	mustAddCommand(parser.AddCommand("list", "List stacks", "", &listCmd{})).Aliases = []string{"ls"}
	mustAddCommand(parser.AddCommand("merge", "Merge PR for current branch", "", &mergeCmd{}))
	mustAddCommand(parser.AddCommand("move", "Move current branch and descendants to a new parent", "", &moveCmd{}))
	mustAddCommand(parser.AddCommand("submit", "Push to remote and open or update PR(s)", "", &submitCmd{}))
	mustAddCommand(parser.AddCommand("refresh", "Refresh remote status for current branch", "", &refreshCmd{})).Hidden = true
	mustAddCommand(parser.AddCommand("restack", "Rebase all branches in the current stack", "", &restackCmd{}))
	mustAddCommand(parser.AddCommand("state", "Manage branch state", "", &stateCmd{})).Hidden = true
	mustAddCommand(parser.AddCommand("sync", "Pull latest PR statuses and sync with local branches", "", &syncCmd{}))

	_, err := parser.ParseArgs(args)
	if err != nil {
		// Handle --help, which is represented as an error by the flags package
		flagsErr := &flags.Error{}
		if errors.As(err, &flagsErr) {
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
