package yascli

import "fmt"

type syncCmd struct{}

func (c *syncCmd) trackUntrackedBranches() error {
	untrackedBranches, err := cmd.yas.UntrackedBranches()
	if err != nil {
		return err
	}

	return cmd.yas.RefreshRemoteStatus(untrackedBranches...)
}

func (c *syncCmd) checkForClosedPRs() error {
	// Fetch latest PR metadata from GitHub for branches that have PRs
	if err := cmd.yas.RefreshRemoteStatus(cmd.yas.TrackedBranches().WithPRs().BranchNames()...); err != nil {
		return err
	}

	// Check for closed PRs here
	for _, branch := range cmd.yas.TrackedBranches().WithPRStatus("closed") {
		// TODO: Prompt
		fmt.Printf("Closed: %s\n", branch.Name)
	}

	return nil
}

func (c *syncCmd) Execute(args []string) error {
	if err := c.trackUntrackedBranches(); err != nil {
		return NewError(err.Error())
	}

	if err := c.checkForClosedPRs(); err != nil {
		return NewError(err.Error())
	}

	return nil
}
