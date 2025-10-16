package yas

import (
	"encoding/json"
	"strings"

	"github.com/dansimau/yas/pkg/log"
	"github.com/dansimau/yas/pkg/xexec"
	"github.com/sourcegraph/conc/pool"
)

func (yas *YAS) fetchGitHubPullRequestStatus(branchName string) (*PullRequestMetadata, error) {
	log.Info("Fetching PRs for branch", branchName)

	b, err := xexec.Command("gh", "pr", "list", "--head", branchName, "--state", "all", "--json", "id,state,url,isDraft,baseRefName").WithStdout(nil).Output()
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

// fetchPRStatusWithChecks fetches PR status including review decision and CI checks.
func (yas *YAS) fetchPRStatusWithChecks(branchName string) (*PullRequestMetadata, error) {
	log.Info("Fetching PR status with checks for branch", branchName)

	b, err := xexec.Command("gh", "pr", "list", "--head", branchName, "--state", "all", "--json", "id,state,url,isDraft,baseRefName,reviewDecision,statusCheckRollup").WithStdout(nil).Output()
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

	// Set parent based on PR base ref name
	if branchMetadata.Parent == "" {
		branchMetadata.Parent = pullRequestMetadata.BaseRefName
	}

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

// refreshPRStatus refreshes PR status including review decision and CI checks.
func (yas *YAS) refreshPRStatus(name string) error {
	if strings.TrimSpace(name) == "" {
		panic("branch name cannot be empty")
	}

	pullRequestMetadata, err := yas.fetchPRStatusWithChecks(name)
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

// RefreshPRStatus refreshes PR status for multiple branches including review and CI status.
func (yas *YAS) RefreshPRStatus(branchNames ...string) error {
	p := pool.New().WithMaxGoroutines(5).WithErrors().WithFirstError()
	for _, name := range branchNames {
		p.Go(func() error {
			return yas.refreshPRStatus(name)
		})
	}

	if err := p.Wait(); err != nil {
		return err
	}

	return nil
}
