package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
)

type mergeCmd struct {
	Arguments struct {
		BranchName string `description:"Branch to merge" positional-arg-name:"branch"`
	} `positional-args:"true"`

	Force bool `description:"Skip CI and review checks" long:"force"`
}

func (c *mergeCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	return yasInstance.Merge(c.Arguments.BranchName, c.Force)
}
