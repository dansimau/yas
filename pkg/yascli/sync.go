package yascli

import (
	"fmt"

	"github.com/dansimau/yas/pkg/yas"
)

type syncCmd struct {
	Restack  bool `description:"Restack branches after sync"   long:"restack"`
	SkipPull bool `description:"Skip pulling the trunk branch" long:"skip-pull"`

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

		// Check if there's a worktree for this branch
		worktreePath, _ := c.yasInstance.WorktreePathForBranch(branch.Name)

		if !cmd.DryRun {
			if err := c.yasInstance.DeleteBranch(branch.Name); err != nil {
				return fmt.Errorf("error deleting branch %s: %w", branch.Name, err)
			}

			if worktreePath != "" {
				fmt.Printf("Deleted branch '%s' and worktree at %s\n", branch.Name, worktreePath)
			} else {
				fmt.Printf("Deleted branch '%s'\n", branch.Name)
			}
		} else {
			if worktreePath != "" {
				fmt.Printf("Would delete branch '%s' and worktree at %s [DRY-RUN]\n", branch.Name, worktreePath)
			} else {
				fmt.Printf("Would delete branch: %s [DRY-RUN]\n", branch.Name)
			}
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
	restackInProgress, err := yasInstance.RestackInProgress()
	if err != nil {
		return fmt.Errorf("failed to check restack state: %w", err)
	}

	if restackInProgress {
		return NewError("a restack operation is already in progress\n\nRun 'yas continue' to resume or 'yas abort' to cancel")
	}

	if err := c.trackUntrackedBranches(); err != nil {
		return NewError(err.Error())
	}

	if !c.SkipPull {
		fmt.Printf("🔄 Pulling %s...\n", yasInstance.Config().TrunkBranch)
	}

	if err := yasInstance.UpdateTrunk(); err != nil {
		return NewError(err.Error())
	}

	if err := c.checkForClosedPRs(); err != nil {
		return NewError(err.Error())
	}

	if c.Restack {
		fmt.Println("🔄 Restacking branches...")

		if err := yasInstance.Restack(yasInstance.Config().TrunkBranch, cmd.DryRun); err != nil {
			return NewError(err.Error())
		}
	}

	return nil
}
