package workyard

import (
	"context"
	"errors"
	"os"
	"runtime"

	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/sourcegraph/conc/pool"
)

// RepoStatus is the current state of a repository in the yard.
type RepoStatus struct {
	Repo Repo
	// Branch is the checked out branch, or "HEAD" when detached.
	Branch string
	Head   string
	Dirty  bool
	// Missing is true when the repository directory no longer exists.
	Missing bool
	Err     error
}

// Status returns the current state of every repository, in path order.
func (y *Yard) Status(ctx context.Context) []RepoStatus {
	statuses := make([]RepoStatus, len(y.Meta.Repos))

	p := pool.New().WithMaxGoroutines(runtime.NumCPU() * 2)

	for i, repo := range y.Meta.Repos {
		p.Go(func() {
			statuses[i] = y.repoStatus(ctx, repo)
		})
	}

	p.Wait()

	return statuses
}

func (y *Yard) repoStatus(ctx context.Context, repo Repo) RepoStatus {
	status := RepoStatus{Repo: repo}
	dir := y.RepoDir(repo)

	if _, err := os.Stat(dir); err != nil {
		status.Missing = errors.Is(err, os.ErrNotExist)
		status.Err = err

		return status
	}

	git := gitexec.WithRepo(dir)
	status.Branch = currentBranch(ctx, dir)

	head, err := git.GetShortHash("HEAD")
	if err != nil {
		status.Err = err

		return status
	}

	status.Head = head

	entries, err := git.StatusEntries()
	if err != nil {
		status.Err = err

		return status
	}

	status.Dirty = len(entries) > 0

	return status
}
