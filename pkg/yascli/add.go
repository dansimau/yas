package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
)

type addCmd struct {
	Branch string `long:"branch" description:"The name of the branch to add to stack (default: current)" required:"false"`
	Parent string `long:"parent" description:"Parent branch name (default: autodetect)" required:"false"`
}

func (c *addCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	return yasInstance.SetParent(c.Branch, c.Parent)
}
