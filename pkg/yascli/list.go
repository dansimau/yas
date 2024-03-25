package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
)

type listCmd struct{}

func (c *listCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	return yasInstance.List()
}
