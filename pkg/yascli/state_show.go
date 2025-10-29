package yascli

import (
	"fmt"

	"github.com/dansimau/yas/pkg/yas"
	"github.com/davecgh/go-spew/spew"
)

type stateShowCmd struct {
	All  bool `description:"Show metadata for all branches" long:"all"`
	Args struct {
		Branches []string `positional-arg-name:"branch" description:"Branch name(s) to show metadata for"`
	} `positional-args:"yes"`
}

func (c *stateShowCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	if c.All {
		branches := yasInstance.TrackedBranches()
		for _, branch := range branches {
			c.Args.Branches = append(c.Args.Branches, branch.Name)
		}
	}

	if len(c.Args.Branches) == 0 {
		currentBranch, err := yasInstance.CurrentBranchName()
		if err != nil {
			return NewError(err.Error())
		}

		c.Args.Branches = []string{currentBranch}
	}

	// First check all branches exist
	for _, branchName := range c.Args.Branches {
		if !yasInstance.BranchExists(branchName) {
			return NewError(fmt.Sprintf("Branch %q is not tracked by yas", branchName))
		}
	}

	data := []any{}

	// Dump metadata for each branch
	for _, branchName := range c.Args.Branches {
		metadata := yasInstance.BranchMetadata(branchName)

		data = append(data, metadata)
	}

	spew.Dump(data)

	return nil
}
