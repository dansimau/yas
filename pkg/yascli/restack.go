package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
)

type restackCmd struct{}

func (c *restackCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	return yasInstance.Restack()
}
