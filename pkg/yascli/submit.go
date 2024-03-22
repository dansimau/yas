package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
)

type submitCmd struct{}

func (c *submitCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	return yasInstance.Submit()
}
