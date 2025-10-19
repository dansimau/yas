package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
)

type refreshCmd struct {
	Arguments struct {
		BranchNames []string `description:"Branch names to refresh" positional-args:"true"`
	} `positional-args:"true"`
}

func (c *refreshCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	return yasInstance.RefreshRemoteStatus(c.Arguments.BranchNames...)
}
