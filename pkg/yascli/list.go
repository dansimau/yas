package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
)

type listCmd struct {
	CurrentStack bool `long:"current-stack" description:"Show only the current stack (ancestors and descendants of current branch)"`
}

func (c *listCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	return yasInstance.List(c.CurrentStack)
}
