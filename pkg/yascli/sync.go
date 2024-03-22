package yascli

import (
	"fmt"

	"github.com/dansimau/yas/pkg/yas"
)

type syncCmd struct {
	yasInstance *yas.YAS
}

func (c *syncCmd) trackUntrackedBranches() error {
	untrackedBranches, err := c.yasInstance.UntrackedBranches()
	if err != nil {
		return err
	}

	return c.yasInstance.RefreshRemoteStatus(untrackedBranches...)
}

func (c *syncCmd) checkForClosedPRs() error {
	fmt.Println("🧹 Checking for merged PRs...")
	// Fetch latest PR metadata from GitHub for branches that have PRs
	if err := c.yasInstance.RefreshRemoteStatus(c.yasInstance.TrackedBranches().WithPRs().BranchNames()...); err != nil {
		return err
	}

	// Check for closed PRs here
	for _, branch := range c.yasInstance.TrackedBranches().WithPRStates("MERGED") {
		if !cmd.DryRun {
			previousRef, err := c.yasInstance.DeleteBranch(branch.Name)
			if err != nil {
				return fmt.Errorf("error deleting branch %s: %w", branch.Name, err)
			}

			if previousRef != "" {
				fmt.Printf("🧹 Deleted branch: %s (ref was: %s)\n", branch.Name, previousRef)
			}
		} else {
			fmt.Printf("🧹 Delete branch: %s [DRY-RUN]\n", branch.Name)
		}
	}

	return nil
}

func (c *syncCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}
	c.yasInstance = yasInstance

	// TODO: Remove - this is for debugging
	if len(args) > 0 {
		return yasInstance.RefreshRemoteStatus(args...)
	}

	if err := c.trackUntrackedBranches(); err != nil {
		return NewError(err.Error())
	}

	if err := c.checkForClosedPRs(); err != nil {
		return NewError(err.Error())
	}

	fmt.Printf("🔄 Pulling %s...\n", yasInstance.Config().TrunkBranch)
	if err := yasInstance.UpdateTrunk(); err != nil {
		return NewError(err.Error())
	}

	return nil
}
