package yascli

import (
	"fmt"

	"github.com/dansimau/yas/pkg/yas"
)

type renameCmd struct {
	Force bool `description:"Rename a branch that has an open pull request (the pull request stays on the old branch name)" long:"force" short:"f"`

	Arguments struct {
		NewName string `description:"New branch name" positional-arg-name:"new-name" required:"true"`
	} `positional-args:"true" required:"true"`
}

func (c *renameCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	oldName, newName, err := yasInstance.RenameCurrentBranch(c.Arguments.NewName, c.Force)
	if err != nil {
		return NewError(err.Error())
	}

	fmt.Printf("Renamed branch '%s' to '%s'\n", oldName, newName)

	return nil
}
