package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
)

type listCmd struct {
	All          bool `description:"Show all local git branches, including untracked ones"                     long:"all"`
	CurrentStack bool `description:"Show only the current stack (ancestors and descendants of current branch)" long:"current-stack"`
	Status       bool `description:"Show PR review and CI status"                                              long:"status"`
}

func (c *listCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	return yasInstance.List(c.CurrentStack, c.Status, c.All)
}
