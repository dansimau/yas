package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
)

type continueCmd struct{}

func (c *continueCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	return yasInstance.Continue()
}
