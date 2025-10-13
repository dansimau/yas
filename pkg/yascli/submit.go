package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
)

type submitCmd struct {
	Stack    bool `description:"Submit all branches in the current stack" long:"stack"`
	Outdated bool `description:"Submit all branches that need submitting" long:"outdated"`
}

func (c *submitCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	if c.Outdated {
		return yasInstance.SubmitOutdated()
	}

	if c.Stack {
		return yasInstance.SubmitStack()
	}

	return yasInstance.Submit()
}
