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

	Parent string `description:"Parent branch name (default: current branch)" long:"parent" required:"false"`
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

	// Check if branch name provided exists locally or remotely
	branchExists, err := yasInstance.BranchExists(c.Arguments.BranchName)
	if err != nil {
		return NewError(err.Error())
	}

	// If the branch exists, switch to it
	if branchExists {
		if err := yasInstance.SwitchBranch(c.Arguments.BranchName); err != nil {
			return NewError(err.Error())
		}
	} else {
		// Otherwise, create it
		if _, err := yasInstance.CreateBranch(c.Arguments.BranchName, c.Parent); err != nil {
			return NewError(err.Error())
		}
	}

	return nil
}
