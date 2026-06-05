// Package yas provides the core business logic for the yas tool.
package yas

import (
	"fmt"
	"os"
	"strings"

	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/go-git/go-git/v5"
	"github.com/hashicorp/go-version"
)

var minimumRequiredGitVersion = version.Must(version.NewVersion("2.38"))

type YAS struct {
	cfg  Config
	data *yasDatabase
	git  *gitexec.Repo
	repo *git.Repository
}

func New(cfg Config) (*YAS, error) {
	repo, err := git.PlainOpen(cfg.RepoDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to open git repo: %w", err)
	}

	statePath, err := resolveStatePath(cfg.RepoDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve state path: %w", err)
	}

	data, err := loadData(statePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load YAS state: %w", err)
	}

	yas := &YAS{
		cfg:  cfg,
		data: data,
		git:  gitexec.WithRepo(cfg.RepoDirectory),
		repo: repo,
	}

	if err := yas.validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	if err := yas.pruneMetadata(); err != nil {
		return nil, fmt.Errorf("failed to prune missing branches: %w", err)
	}

	return yas, nil
}

func NewFromRepository(repoDirectory string) (*YAS, error) {
	cfg, err := ReadConfig(repoDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	return New(*cfg)
}

func (yas *YAS) Config() Config {
	return yas.cfg
}

// UpdateConfig sets the new config and writes it to the configuration file.
func (yas *YAS) UpdateConfig(cfg Config) (string, error) {
	yas.cfg = cfg

	return WriteConfig(cfg)
}

// BranchMetadata returns the metadata for the specified branch.
func (yas *YAS) BranchMetadata(branchName string) BranchMetadata {
	return yas.data.Branches.Get(branchName)
}

// CurrentBranchName returns the name of the current branch.
func (yas *YAS) CurrentBranchName() (string, error) {
	return yas.git.GetCurrentBranchName()
}

func (yas *YAS) validate() error {
	gitVersion, err := yas.git.GitVersion()
	if err != nil {
		return fmt.Errorf("unable to determine git version: %w", err)
	}

	if gitVersion.LessThan(minimumRequiredGitVersion) {
		path, _ := yas.git.GitPath()

		return fmt.Errorf("git version %s (%s) is less than the required version %s", gitVersion.String(), path, minimumRequiredGitVersion.String())
	}

	return nil
}

// CreateBranch creates a new branch with the given name, optionally applying a user prefix.
// If parentBranch is empty, it uses the current branch as the parent.
// The new branch is created, checked out, and added to the stack.
// If there are staged changes, they are automatically committed.
func (yas *YAS) CreateBranch(branchName string, parentBranch string) (string, error) {
	// Determine full branch name (with or without prefix based on config)
	fullBranchName := branchName

	if yas.cfg.AutoPrefixBranch {
		// Get git email to determine prefix
		// Check GIT_AUTHOR_EMAIL env var first, then fall back to git config
		email := os.Getenv("GIT_AUTHOR_EMAIL")
		if email == "" {
			var err error

			email, err = yas.git.GetConfig("user.email")
			if err != nil {
				return "", fmt.Errorf("failed to get git user.email: %w", err)
			}
		}

		// Extract username from email (part before @)
		username := email
		if idx := strings.Index(email, "@"); idx != -1 {
			username = email[:idx]
		}

		// Create full branch name with username prefix
		fullBranchName = fmt.Sprintf("%s/%s", username, branchName)
	}

	// Determine the current branch so we can decide where to base the new
	// branch from. When no parent is given we require it (it becomes the
	// parent). When an explicit parent is given we don't strictly need it, so
	// we ignore errors here (e.g. detached HEAD): it's only used to decide
	// whether to branch from the parent rather than the current HEAD.
	var currentBranch string

	if parentBranch == "" {
		var err error

		currentBranch, err = yas.git.GetCurrentBranchName()
		if err != nil {
			return "", fmt.Errorf("failed to get current branch: %w", err)
		}

		// Use current branch as parent
		parentBranch = currentBranch
	} else {
		currentBranch, _ = yas.git.GetCurrentBranchName()
	}

	// When an explicit parent different from the current branch is requested,
	// create a fresh branch based on that parent rather than the current HEAD.
	// In that case we do not auto-commit; any staged changes are preserved in
	// the index (git only carries them across when they don't conflict with
	// the switch) rather than being silently discarded.
	createFromParent := parentBranch != currentBranch

	// Create the new branch
	if createFromParent {
		// Resolve the parent to a ref git can use as a start point. When the
		// parent only exists remotely (e.g. origin/main with no local main),
		// fall back to the remote-tracking ref.
		startPoint, err := yas.resolveBranchStartPoint(parentBranch)
		if err != nil {
			return "", err
		}

		if err := yas.git.CreateBranchFrom(fullBranchName, startPoint); err != nil {
			return "", fmt.Errorf("failed to create branch: %w", err)
		}
	} else {
		if err := yas.git.CreateBranch(fullBranchName); err != nil {
			return "", fmt.Errorf("failed to create branch: %w", err)
		}
	}

	// Add to stack with parent
	if err := yas.SetParent(fullBranchName, parentBranch, ""); err != nil {
		return "", err
	}

	// Check for staged changes and commit automatically. This is skipped when
	// the branch was created from a different parent, since in that case we
	// want a fresh branch and leave any staged changes untouched.
	if !createFromParent {
		hasStagedChanges, err := yas.git.HasStagedChanges()
		if err != nil {
			return "", fmt.Errorf("failed to check for staged changes: %w", err)
		}

		if hasStagedChanges {
			if err := yas.git.Commit(); err != nil {
				return "", fmt.Errorf("failed to commit staged changes: %w", err)
			}
		}
	}

	return fullBranchName, nil
}

// resolveBranchStartPoint resolves a parent branch name to a git ref usable as
// the start point for a new branch. It prefers a local branch, then falls back
// to the remote-tracking ref (origin/<branch>) for parents that only exist
// remotely. If neither exists it returns the name unchanged so git can surface
// a clear error.
func (yas *YAS) resolveBranchStartPoint(parentBranch string) (string, error) {
	localExists, err := yas.git.BranchExists(parentBranch)
	if err != nil {
		return "", fmt.Errorf("failed to check if parent branch exists: %w", err)
	}

	if localExists {
		return parentBranch, nil
	}

	remoteExists, err := yas.git.RemoteBranchExists(parentBranch)
	if err != nil {
		return "", fmt.Errorf("failed to check if parent branch exists remotely: %w", err)
	}

	if remoteExists {
		return "origin/" + parentBranch, nil
	}

	return parentBranch, nil
}

func (yas *YAS) Reload() error {
	return yas.data.Reload()
}
