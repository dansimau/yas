package yas

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/dansimau/yas/pkg/log"
	"github.com/dansimau/yas/pkg/xexec"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/sourcegraph/conc/pool"
)

const yasStateFile = ".git/.yasstate"

type YAS struct {
	cfg  Config
	data *yasDatabase
	git  *gitexec.Repo
	repo *git.Repository
}

func New(cfg Config) (*YAS, error) {
	repo, err := git.PlainOpen(cfg.RepoDirectory)
	if err != nil {
		return nil, err
	}

	data, err := loadData(path.Join(cfg.RepoDirectory, yasStateFile))
	if err != nil {
		return nil, err
	}

	return &YAS{
		cfg:  cfg,
		data: data,
		git:  gitexec.WithRepo(cfg.RepoDirectory),
		repo: repo,
	}, nil
}

func NewFromRepository(repoDirectory string) (*YAS, error) {
	cfg, err := ReadConfig(repoDirectory)
	if err != nil {
		return nil, err
	}

	return New(*cfg)
}

func (yas *YAS) Config() Config {
	return yas.cfg
}

func (yas *YAS) DeleteBranch(name string) (previousRef string, err error) {
	branchExists, err := yas.git.BranchExists(name)
	if err != nil {
		return "", err
	}

	if !branchExists {
		if err := yas.cleanupBranch(name); err != nil {
			return "", err
		}

		return "", nil
	}

	currentBranchName, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return "", err
	}

	// Can't delete the branch while we're on it; switch to trunk
	if currentBranchName == name {
		if err := yas.git.Checkout(yas.cfg.TrunkBranch); err != nil {
			return "", fmt.Errorf("can't delete branch while on it; failed to checkout trunk: %w", err)
		}
	}

	// Get the ref of the branch before we delete it, so we can return/print it
	// which allows the person to undo.
	existingRefShortHash, err := yas.git.GetShortHash(name)
	if err != nil {
		return "", err
	}

	if err := yas.git.DeleteBranch(name); err != nil {
		return "", err
	}

	// If this fails, make it a warning, since the most important thing here is
	// to return the previous hash now that the branch has been removed
	yas.cleanupBranch(name) // TODO: emit warning if this fails

	return strings.TrimSpace(string(existingRefShortHash)), nil
}

func (yas *YAS) Submit() error {
	currentBranch, err := yas.git.GetCurrentBranchName()
	if err != nil {
		return err
	}

	if currentBranch == "HEAD" {
		return errors.New("cannot submit in detached HEAD state")
	}

	if err := yas.refreshRemoteStatus(currentBranch); err != nil {
		return err
	}

	if err := yas.git.Push(); err != nil {
		return fmt.Errorf("failed to push: %w", err)
	}

	if err := xexec.Command("gh", "pr", "create", "--draft", "--fill-first").Run(); err != nil {
		return err
	}

	return nil
}

// UpdateConfig sets the new config and writes it to the configuration file.
func (yas *YAS) UpdateConfig(cfg Config) (string, error) {
	yas.cfg = cfg
	return WriteConfig(cfg)
}

func (yas *YAS) TrackedBranches() Branches {
	return yas.data.Branches.ToSlice()
}

func (yas *YAS) UntrackedBranches() ([]string, error) {
	iter, err := yas.repo.Branches()
	if err != nil {
		return nil, err
	}

	branches := []string{}
	iter.ForEach(func(r *plumbing.Reference) error {
		name := string(r.Name().Short())
		if !yas.data.Branches.Exists(name) {
			branches = append(branches, name)
		}
		return nil
	})

	return branches, nil
}

func (yas *YAS) refreshRemoteStatus(name string) error {
	if strings.TrimSpace(name) == "" {
		panic("branch name cannot be empty")
	}

	pullRequestMetadata, err := yas.fetchGitHubPullRequestStatus(name)
	if err != nil {
		return err
	}

	if pullRequestMetadata == nil {
		pullRequestMetadata = &PullRequestMetadata{}
	}

	branchMetadata := yas.data.Branches.Get(name)

	branchMetadata.GitHubPullRequest = *pullRequestMetadata

	yas.data.Branches.Set(name, branchMetadata)

	if err := yas.data.Save(); err != nil {
		return err
	}

	return nil
}

func (yas *YAS) RefreshRemoteStatus(branchNames ...string) error {
	p := pool.New().WithMaxGoroutines(5).WithErrors().WithFirstError()
	for _, name := range branchNames {
		p.Go(func() error {
			return yas.refreshRemoteStatus(name)
		})
	}

	if err := p.Wait(); err != nil {
		return err
	}

	return nil
}

func (yas *YAS) cleanupBranch(name string) error {
	yas.data.Branches.Remove(name)
	return yas.data.Save()
}

func (yas *YAS) UpdateTrunk() error {
	if err := yas.git.Checkout(yas.cfg.TrunkBranch); err != nil {
		return err
	}

	// Switch back to original branch
	defer yas.git.Checkout("-")

	return yas.git.Pull()
}

func (yas *YAS) fetchGitHubPullRequestStatus(branchName string) (*PullRequestMetadata, error) {
	log.Info("Fetching PRs for branch", branchName)

	b, err := xexec.Command("gh", "pr", "list", "--head", branchName, "--state", "all", "--json", "id,state").WithStdout(nil).Output()
	if err != nil {
		return nil, err
	}

	data := []PullRequestMetadata{}
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, nil
	}

	return &data[0], nil
}
