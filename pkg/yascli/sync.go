package yascli

import (
	"fmt"
)

type syncCmd struct{}

func (c *syncCmd) trackUntrackedBranches() error {
	untrackedBranches, err := cmd.yas.UntrackedBranches()
	if err != nil {
		return err
	}

	return cmd.yas.RefreshRemoteStatus(untrackedBranches...)
}

func (c *syncCmd) checkForClosedPRs() error {
	fmt.Println("🧹 Checking for merged PRs...")
	// Fetch latest PR metadata from GitHub for branches that have PRs
	if err := cmd.yas.RefreshRemoteStatus(cmd.yas.TrackedBranches().WithPRs().BranchNames()...); err != nil {
		return err
	}

	// Check for closed PRs here
	for _, branch := range cmd.yas.TrackedBranches().WithPRStates("MERGED") {
		if !cmd.DryRun {
			previousRef, err := cmd.yas.DeleteBranch(branch.Name)
			if err != nil {
				return fmt.Errorf("error deleting branch %s: %w", branch.Name, err)
			}

			fmt.Printf("🧹 Deleted branch: %s (ref was: %s)\n", branch.Name, previousRef)
		} else {
			fmt.Printf("🧹 Delete branch: %s [DRY-RUN]\n", branch.Name)
		}
	}

	return nil
}

func (c *syncCmd) Execute(args []string) error {
	// TODO: Remove - this is for debugging
	if len(args) > 0 {
		return cmd.yas.RefreshRemoteStatus(args...)
	}

	if err := c.trackUntrackedBranches(); err != nil {
		return NewError(err.Error())
	}

	if err := c.checkForClosedPRs(); err != nil {
		return NewError(err.Error())
	}

	return nil
}
