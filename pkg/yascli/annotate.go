package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
)

type annotateCmd struct {
	All bool `long:"all" description:"Annotate all branches with PRs"`
}

func (c *annotateCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	if c.All {
		return yasInstance.AnnotateAll()
	}

	return yasInstance.Annotate()
}
