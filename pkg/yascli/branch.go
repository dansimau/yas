package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
)

const branchCmdLongHelp = `
Checkout/switch to a branch:
- yas branch [existing-local-or-remote-branch-name]
- yas branch (With no arguments, will open interactive branch switcher)

Create a new branch:
- yas branch <new-branch-name>`

type branchCmd struct {
	Arguments struct {
		BranchName string `description:"Branch name" positional-args:"true"`
	} `positional-args:"true"`

	Parent   string `description:"Parent branch name (default: current branch)" long:"parent"   required:"false"`
	Worktree bool   `description:"Create branch in a new worktree"              long:"worktree"`
}

func (c *branchCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	// If no args provided, show interactive branch switcher
	if c.Arguments.BranchName == "" {
		if err := yasInstance.SwitchBranchInteractive(); err != nil {
			return NewError(err.Error())
		}

		return nil
	}

	fullBranchName := c.Arguments.BranchName

	branchExistsLocally, err := yasInstance.BranchExistsLocally(c.Arguments.BranchName)
	if err != nil {
		return NewError(err.Error())
	}

	branchExistsRemotely, err := yasInstance.BranchExistsRemotely(c.Arguments.BranchName)
	if err != nil {
		return NewError(err.Error())
	}

	switch {
	// Create branch if it doesn't exist anywhere
	case !branchExistsLocally && !branchExistsRemotely:
		fullBranchName, err = yasInstance.CreateBranch(c.Arguments.BranchName, c.Parent)
		if err != nil {
			return NewError(err.Error())
		}

	// Branch only exists on the remote: create a local branch that tracks it,
	// rather than leaving it to git's DWIM (which gives up when the branch name
	// exists on more than one remote).
	case !branchExistsLocally:
		if err := yasInstance.AdoptRemoteBranch(fullBranchName); err != nil {
			return NewError(err.Error())
		}
	}

	// Determine if we should use worktree: either explicitly via flag or via config
	useWorktree := c.Worktree || yasInstance.Config().WorktreeBranch

	// Ensure worktree exists for branch
	if useWorktree {
		if err := yasInstance.EnsureLinkedWorktreeForBranch(fullBranchName); err != nil {
			return NewError(err.Error())
		}
	}

	// Switch to the branch
	if err := yasInstance.SwitchBranch(fullBranchName); err != nil {
		return NewError(err.Error())
	}

	return nil
}
