package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
)

type linkCmd struct {
	Unlink bool `description:"Remove the PRs of the current stack from their stack on GitHub" long:"unlink"`
}

func (c *linkCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	if c.Unlink {
		return yasInstance.Unlink(cmd.DryRun)
	}

	return yasInstance.Link(cmd.DryRun)
}
