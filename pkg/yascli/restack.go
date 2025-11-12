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

	if c.All {
		if c.Args.Branch != "" {
			return NewError("specifying a branch is incompatible with --all")
		}

		c.Args.Branch = yasInstance.Config().TrunkBranch
	}

	return yasInstance.Restack(c.Args.Branch, c.DryRun)
}
