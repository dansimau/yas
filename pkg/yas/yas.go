package yas

import (
	"encoding/json"
	"path"

	"github.com/dansimau/yas/pkg/log"
	"github.com/dansimau/yas/pkg/xexec"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/sourcegraph/conc/pool"
)

type YAS struct {
	cfg  Config
	data *yasDatabase
	repo *git.Repository
}

func New(cfg Config) (*YAS, error) {
	repo, err := git.PlainOpen(cfg.RepoDirectory)
	if err != nil {
		return nil, err
	}

	data, err := loadData(path.Join(cfg.RepoDirectory, ".yasstate"))
	if err != nil {
		return nil, err
	}

	return &YAS{
		cfg:  cfg,
		data: data,
		repo: repo,
	}, nil
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
	p := pool.New().WithErrors().WithFirstError()
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

func (yas *YAS) fetchGitHubPullRequestStatus(branchName string) (*PullRequestMetadata, error) {
	log.Info("Fetching PRs for branch", branchName)

	b, err := xexec.Command("gh", "pr", "list", "--author", "@me", "--head", branchName, "--state", "all", "--json", "id,state").WithStdout(nil).Output()
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
