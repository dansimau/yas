package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
)

type addCmd struct {
	Parent      string `description:"Parent branch name (default: autodetect)"                           long:"parent"       required:"false"`
	BranchPoint string `description:"Commit SHA where branch diverged from parent (default: autodetect)" long:"branch-point" required:"false"`

	Args struct {
		Branch string `description:"The name of the branch to add to stack (default: current)" positional-arg-name:"branch"`
	} `positional-args:"true"`
}

func (c *addCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	return yasInstance.SetParent(c.Args.Branch, c.Parent, c.BranchPoint)
}
