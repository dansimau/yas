package yascli

import (
	"fmt"

	"github.com/dansimau/yas/pkg/cliutil"
	"github.com/dansimau/yas/pkg/yas"
)

type deleteCmd struct {
	Force bool `description:"Force deletion (skip confirmation, remove dirty worktrees)" long:"force" short:"f"`

	Arguments struct {
		BranchName string `description:"Branch to delete (defaults to current branch)" positional-arg-name:"branch"`
	} `positional-args:"true"`
}

func (c *deleteCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	branchName := c.Arguments.BranchName

	// Default to current branch if none specified
	if branchName == "" {
		currentBranch, err := yasInstance.CurrentBranchName()
		if err != nil {
			return NewError(fmt.Sprintf("failed to get current branch: %v", err))
		}

		branchName = currentBranch
	}

	// Prevent deleting the trunk branch
	if branchName == yasInstance.Config().TrunkBranch {
		return NewError(fmt.Sprintf("cannot delete trunk branch '%s'", branchName))
	}

	// Check if there's a worktree for this branch
	worktreePath, err := yasInstance.WorktreePathForBranch(branchName)
	if err != nil {
		return NewError(fmt.Sprintf("failed to check for worktree: %v", err))
	}

	// Show confirmation unless --force is used
	if !c.Force {
		var promptMsg string
		if worktreePath != "" {
			promptMsg = fmt.Sprintf("Delete branch '%s' at %s? (y/N)", branchName, worktreePath)
		} else {
			promptMsg = fmt.Sprintf("Delete branch '%s'? (y/N)", branchName)
		}

		if !cliutil.Confirm(promptMsg) {
			fmt.Println("Aborting.")

			return nil
		}
	}

	if err := yasInstance.DeleteBranchWithWorktree(branchName, c.Force); err != nil {
		return NewError(fmt.Sprintf("failed to delete branch: %v", err))
	}

	if worktreePath != "" {
		fmt.Printf("Deleted branch '%s' and worktree at %s\n", branchName, worktreePath)
	} else {
		fmt.Printf("Deleted branch '%s'\n", branchName)
	}

	return nil
}
