package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
)

type restackCmd struct {
	All    bool `description:"Restack all branches"                               long:"all"`
	DryRun bool `description:"Don't make any changes, just show what will happen" long:"dry-run"`

	Args struct {
		Branch string `description:"The name of the branch to restack (default: current)" positional-arg-name:"branch"`
	} `positional-args:"true"`
}

func (c *restackCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	branch := c.Args.Branch

	if c.All {
		if branch != "" {
			return NewError("specifying a branch is incompatible with --all")
		}

		branch = yasInstance.Config().TrunkBranch
	} else if branch == "" {
		// Default to current branch when no branch specified and --all not used
		currentBranch, err := yasInstance.CurrentBranchName()
		if err != nil {
			return NewError(err.Error())
		}

		branch = currentBranch
	}

	return yasInstance.Restack(branch, c.DryRun)
}
