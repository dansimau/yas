package yascli

import (
	"fmt"

	"github.com/dansimau/yas/pkg/yas"
)

type syncCmd struct {
	SkipRestack bool `description:"Skip restacking branches after sync" long:"skip-restack"`

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

		if !cmd.DryRun {
			if err := c.yasInstance.DeleteMergedBranch(branch.Name); err != nil {
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

	// Check if a restack is in progress
	if yas.RestackStateExists(cmd.RepoDirectory) {
		return NewError("a restack operation is already in progress\n\nRun 'yas continue' to resume or 'yas abort' to cancel")
	}

	if len(args) > 0 {
		return yasInstance.RefreshRemoteStatus(args...)
	}

	if err := c.trackUntrackedBranches(); err != nil {
		return NewError(err.Error())
	}

	fmt.Printf("🔄 Pulling %s...\n", yasInstance.Config().TrunkBranch)

	if err := yasInstance.UpdateTrunk(); err != nil {
		return NewError(err.Error())
	}

	if err := c.checkForClosedPRs(); err != nil {
		return NewError(err.Error())
	}

	if !c.SkipRestack {
		fmt.Println("🔄 Restacking branches...")

		if err := yasInstance.Restack(); err != nil {
			return NewError(err.Error())
		}
	}

	return nil
}
