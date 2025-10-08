package yascli

import (
	"fmt"
	"os"
	"strings"

	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/dansimau/yas/pkg/yas"
)

type branchCmd struct {
	Parent string `long:"parent" description:"Parent branch name (default: current branch)" required:"false"`
}

func (c *branchCmd) Execute(args []string) error {
	if len(args) == 0 {
		return NewError("branch name is required")
	}

	branchName := args[0]

	// Get git email to determine prefix
	// Check GIT_AUTHOR_EMAIL env var first, then fall back to git config
	git := gitexec.WithRepo(cmd.RepoDirectory)
	email := os.Getenv("GIT_AUTHOR_EMAIL")
	if email == "" {
		var err error
		email, err = git.GetConfig("user.email")
		if err != nil {
			return NewError(fmt.Sprintf("failed to get git user.email: %v", err))
		}
	}

	// Extract username from email (part before @)
	username := email
	if idx := strings.Index(email, "@"); idx != -1 {
		username = email[:idx]
	}

	// Create full branch name with username prefix
	fullBranchName := fmt.Sprintf("%s/%s", username, branchName)

	// Determine parent branch
	parentBranch := c.Parent
	if parentBranch == "" {
		// Use current branch as parent
		currentBranch, err := git.GetCurrentBranchName()
		if err != nil {
			return NewError(fmt.Sprintf("failed to get current branch: %v", err))
		}
		parentBranch = currentBranch
	}

	// Create the new branch
	if err := git.CreateBranch(fullBranchName); err != nil {
		return NewError(fmt.Sprintf("failed to create branch: %v", err))
	}

	// Checkout the new branch
	if err := git.Checkout(fullBranchName); err != nil {
		return NewError(fmt.Sprintf("failed to checkout branch: %v", err))
	}

	fmt.Printf("Created and checked out branch: %s\n", fullBranchName)

	// Add to stack with parent
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	if err := yasInstance.SetParent(fullBranchName, parentBranch, ""); err != nil {
		return NewError(err.Error())
	}

	return nil
}
