package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
)

type moveCmd struct {
	Arguments struct {
		TargetBranch string `description:"Branch to move (defaults to current branch)"`
	} `positional-args:"true"`

	Onto string `description:"Target branch to rebase onto" long:"onto" required:"true"`
}

func (c *moveCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	targetBranch := yasInstance.ResolveTrunkAlias(c.Arguments.TargetBranch)
	onto := yasInstance.ResolveTrunkAlias(c.Onto)

	return yasInstance.MoveBranch(targetBranch, onto)
}
