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
		// Don't delete the trunk branch
		if branch.Name == c.yasInstance.Config().TrunkBranch {
			continue
		}

		parentBranch := branch.Parent
		if parentBranch == "" {
			parentBranch = c.yasInstance.Config().TrunkBranch
		}

		for _, child := range c.yasInstance.TrackedBranches().WithParent(branch.Name) {
			if cmd.DryRun {
				fmt.Printf("Would restack %s onto %s [DRY-RUN]\n", child.Name, parentBranch)
				continue
			}

			fmt.Printf("🔁 Restacking %s onto %s...\n", child.Name, parentBranch)

			if err := c.yasInstance.SetParent(child.Name, parentBranch); err != nil {
				return fmt.Errorf("error updating parent for %s: %w", child.Name, err)
			}

			if err := c.yasInstance.RestackBranchOntoParent(child.Name); err != nil {
				return fmt.Errorf("error restacking %s: %w", child.Name, err)
			}
		}

		if !cmd.DryRun {
			if err := c.yasInstance.DeleteBranch(branch.Name); err != nil {
				return fmt.Errorf("error deleting branch %s: %w", branch.Name, err)
			}
		} else {
			fmt.Printf("Would delete branch: %s [DRY-RUN]\n", branch.Name)
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
