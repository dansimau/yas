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

	// Adopt the PR base ref as the parent for branches that aren't tracked yet.
	// This must happen before the branch metadata is stored, otherwise the
	// parent is written to a discarded copy.
	if branchMetadata.Parent == "" {
		yas.adoptPullRequestBaseAsParent(&branchMetadata)
	}

	yas.data.Branches.Set(name, branchMetadata)

	if err := yas.data.Save(); err != nil {
		return err
	}

	// Now that we know what's on the remote, make sure the branch tracks it. An
	// open PR means the remote branch is there even if we have never fetched it,
	// so it's worth fetching to set tracking up. Merged branches are on their
	// way out, so leave them alone.
	if branchMetadata.GitHubPullRequest.State != "MERGED" {
		yas.ensureUpstreamTracking(name, branchMetadata.GitHubPullRequest.State == "OPEN")
	}

	return nil
}

// adoptPullRequestBaseAsParent records the base ref of a branch's PR as its
// parent, which is how branches that were never explicitly added become part of
// the branch graph. A branch is only usable in the graph if its branch point is
// known as well, so the parent is left unset when it cannot be determined.
func (yas *YAS) adoptPullRequestBaseAsParent(branchMetadata *BranchMetadata) {
	baseRefName := branchMetadata.GitHubPullRequest.BaseRefName

	// The trunk branch never has a parent, and no branch can be its own parent.
	if baseRefName == "" || baseRefName == branchMetadata.Name || branchMetadata.Name == yas.cfg.TrunkBranch {
		return
	}

	branchPoint, err := yas.detectBranchPoint(branchMetadata.Name, baseRefName)
	if err != nil || branchPoint == "" {
		log.Info("Unable to determine branch point for", branchMetadata.Name, "- leaving it untracked:", err)

		return
	}

	branchMetadata.Parent = baseRefName
	branchMetadata.BranchPoint = branchPoint
}

func (yas *YAS) RefreshRemoteStatus(branchNames ...string) error {
	// Refresh current branch if no branches are provided
	if len(branchNames) == 0 {
		currentBranch, err := yas.git.GetCurrentBranchName()
		if err != nil {
			return err
		}

		branchNames = append(branchNames, currentBranch)
	}

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

func (yas *YAS) prStateToYasState(pullRequestMetadata PullRequestMetadata) string {
	if pullRequestMetadata.IsDraft {
		return "DRAFT"
	}

	return pullRequestMetadata.State
}
