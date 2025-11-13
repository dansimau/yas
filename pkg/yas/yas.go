// Package yas provides the core business logic for the yas tool.
package yas

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/dansimau/yas/pkg/fsutil"
	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/go-git/go-git/v5"
	"github.com/hashicorp/go-version"
)

var minimumRequiredGitVersion = version.Must(version.NewVersion("2.38"))

var yasStateFiles = []string{".yas/yas.state.json", ".git/.yasstate"}

// resolveStatePath returns the first state path that exists, or the first
// path if none exist (for writing to the new location).
func resolveStatePath(repoDir string) (string, error) {
	for _, filename := range yasStateFiles {
		fullPath := path.Join(repoDir, filename)

		exists, err := fsutil.FileExists(fullPath)
		if err != nil {
			return "", err
		}

		if exists {
			return fullPath, nil
		}
	}
	// No file exists - use first (new) path for writing
	return path.Join(repoDir, yasStateFiles[0]), nil
}

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

	// Determine parent branch
	if parentBranch == "" {
		// Use current branch as parent
		currentBranch, err := yas.git.GetCurrentBranchName()
		if err != nil {
			return "", fmt.Errorf("failed to get current branch: %w", err)
		}

		parentBranch = currentBranch
	}

	// Create the new branch
	if err := yas.git.CreateBranch(fullBranchName); err != nil {
		return "", fmt.Errorf("failed to create branch: %w", err)
	}

	// Add to stack with parent
	if err := yas.SetParent(fullBranchName, parentBranch, ""); err != nil {
		return "", err
	}

	// Check for staged changes and commit automatically
	hasStagedChanges, err := yas.git.HasStagedChanges()
	if err != nil {
		return "", fmt.Errorf("failed to check for staged changes: %w", err)
	}

	if hasStagedChanges {
		if err := yas.git.Commit(); err != nil {
			return "", fmt.Errorf("failed to commit staged changes: %w", err)
		}
	}

	return fullBranchName, nil
}
