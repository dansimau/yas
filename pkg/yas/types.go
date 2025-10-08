package yas

import (
	"slices"

	"github.com/dansimau/yas/pkg/sliceutil"
)

type BranchMetadata struct {
	Name              string
	GitHubPullRequest PullRequestMetadata
	Parent            string `json:",omitempty"`
}

type PullRequestMetadata struct {
	ID      string
	State   string
	URL     string
	IsDraft bool
}

type Branches []BranchMetadata

func (b Branches) filter(test func(BranchMetadata) bool) Branches {
	return sliceutil.Filter(b, test)
}

func (b Branches) BranchNames() []string {
	result := []string{}
	for _, branch := range b {
		result = append(result, branch.Name)
	}
	return result
}

func (b Branches) Get(name string) (branch BranchMetadata, exists bool) {
	result := b.filter(func(bm BranchMetadata) bool {
		return bm.Name == name
	})

	if len(result) == 0 {
		return BranchMetadata{}, false
	}

	return result[0], true
}

func (b *Branches) Set(data BranchMetadata) {
	if data.Name == "" {
		panic("branch name is empty")
	}

	// Remove existing entries with the same name
	n := b.filter(func(bm BranchMetadata) bool {
		return bm.Name != data.Name
	})

	// Add new entry
	n = append(n, data)

	*b = n
}

func (b Branches) WithParents() Branches {
	return b.filter(func(b BranchMetadata) bool {
		return b.Parent != ""
	})
}

func (b Branches) WithPRs() Branches {
	return b.filter(func(b BranchMetadata) bool {
		return b.GitHubPullRequest.ID != ""
	})
}

func (b Branches) WithPRStates(states ...string) Branches {
	return b.filter(func(b BranchMetadata) bool {
		return slices.Contains(states, b.GitHubPullRequest.State)
	})
}
