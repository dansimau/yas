package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
)

type restackCmd struct {
	DryRun bool `description:"Don't make any changes, just show what will happen" long:"dry-run"`
}

func (c *restackCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	return yasInstance.Restack(c.DryRun)
}
