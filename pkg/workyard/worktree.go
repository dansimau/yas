package workyard

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/sourcegraph/conc/pool"
)

// RefAction is how a repository's worktree will be checked out.
type RefAction int

const (
	// ActionExisting checks out an existing local branch.
	ActionExisting RefAction = iota
	// ActionTrack creates a local branch tracking StartPoint (a remote ref).
	ActionTrack
	// ActionDetach checks out StartPoint with a detached HEAD.
	ActionDetach
	// ActionCreate creates the branch from StartPoint (the trunk).
	ActionCreate
)

// RepoPlan is the planned worktree for one repository.
type RepoPlan struct {
	Repo       Repo
	Action     RefAction
	StartPoint string
	// Err is set when no worktree can be created for the repository.
	Err error
}

// Describe returns a one-line description of the planned action.
func (p *RepoPlan) Describe() string {
	if p.Err != nil {
		return "error: " + p.Err.Error()
	}

	switch p.Action {
	case ActionExisting:
		return "check out existing branch " + p.Repo.Ref
	case ActionTrack:
		return fmt.Sprintf("create branch %s tracking %s", p.Repo.Ref, p.StartPoint)
	case ActionDetach:
		return "detached at " + p.StartPoint
	case ActionCreate:
		return fmt.Sprintf("create branch %s from %s", p.Repo.Ref, p.StartPoint)
	}

	return "unknown"
}

// resolveRefs decides, for every repository in the plan, how the branch will
// be checked out. Repositories whose branch cannot be resolved get Err set.
func resolveRefs(ctx context.Context, plan *Plan, cfg Config, jobs int) {
	p := pool.New().WithMaxGoroutines(jobs).WithContext(ctx)

	for _, repo := range plan.Repos {
		p.Go(func(context.Context) error {
			repo.Repo.Ref = plan.Branch
			repo.Err = resolveRef(repo, cfg)

			return nil
		})
	}

	_ = p.Wait()

	// A branch can only be checked out in one worktree per repository, so
	// when several source repositories share a git directory (they are
	// worktrees of each other) only the first gets the branch and the rest
	// are detached at the same start point.
	sort.Slice(plan.Repos, func(i, j int) bool { return plan.Repos[i].Repo.Path < plan.Repos[j].Repo.Path })

	claimed := map[string]bool{}

	for _, repo := range plan.Repos {
		if repo.Err != nil || repo.Action == ActionDetach {
			continue
		}

		if claimed[repo.Repo.CommonDir] {
			if repo.Action == ActionExisting {
				repo.StartPoint = repo.Repo.Ref
			}

			repo.Action = ActionDetach

			continue
		}

		claimed[repo.Repo.CommonDir] = true
	}
}

func resolveRef(repo *RepoPlan, cfg Config) error {
	git := gitexec.WithRepo(repo.Repo.Source)
	ref := repo.Repo.Ref

	commonDir, err := git.CommonDir()
	if err != nil {
		return fmt.Errorf("not a git repository: %w", err)
	}

	repo.Repo.CommonDir = commonDir

	// 1. Existing local branch.
	exists, err := git.BranchExists(ref)
	if err != nil {
		return err
	}

	if exists {
		worktrees, err := git.Worktrees()
		if err != nil {
			return err
		}

		for _, wt := range worktrees {
			if wt.Branch == ref {
				return fmt.Errorf("branch %q is already checked out at %s (hint: use a new branch name)", ref, wt.Path)
			}
		}

		repo.Action = ActionExisting

		return nil
	}

	// 2. Branch on exactly one remote.
	remoteRefs, err := git.RemoteBranchRefs(ref)
	if err != nil {
		return err
	}

	switch len(remoteRefs) {
	case 0:
	case 1:
		repo.Action = ActionTrack
		repo.StartPoint = remoteRefs[0]

		return nil
	default:
		return fmt.Errorf("branch %q exists on more than one remote (%s)", ref, strings.Join(remoteRefs, ", "))
	}

	// 3. Tag or commit.
	isCommit, err := git.IsCommitish(ref)
	if err != nil {
		return err
	}

	if isCommit {
		repo.Action = ActionDetach
		repo.StartPoint = ref

		return nil
	}

	// 4. Create from trunk.
	trunk := cfg.trunkFor(repo.Repo.Path)
	if trunk == "" {
		trunk, err = git.DetectMainBranch()
		if err != nil {
			return err
		}
	}

	if trunk == "" {
		return fmt.Errorf("branch %q not found and no trunk branch to create it from (hint: set trunk in %s/%s)", ref, workyardDir, configFile)
	}

	isCommit, err = git.IsCommitish(trunk)
	if err != nil {
		return err
	}

	if !isCommit {
		return fmt.Errorf("branch %q not found and trunk %q does not exist", ref, trunk)
	}

	repo.Action = ActionCreate
	repo.StartPoint = trunk

	return nil
}

// addWorktree creates the planned worktree at dst and records the outcome in
// repo.Repo.
func addWorktree(repo *RepoPlan, dst string) error {
	git := gitexec.WithRepo(repo.Repo.Source)

	var err error

	switch repo.Action {
	case ActionExisting:
		err = git.WorktreeAddExisting(dst, repo.Repo.Ref)
	case ActionTrack:
		err = git.WorktreeAddTracking(dst, repo.Repo.Ref, repo.StartPoint)
	case ActionDetach:
		err = git.WorktreeAddDetached(dst, repo.StartPoint)
	case ActionCreate:
		err = git.WorktreeAdd(dst, repo.Repo.Ref, repo.StartPoint)
	}

	if err != nil {
		return err
	}

	switch repo.Action {
	case ActionTrack, ActionCreate:
		repo.Repo.CreatedBranch = true
	case ActionDetach:
		repo.Repo.Detached = true
		repo.Repo.Ref = repo.StartPoint
	case ActionExisting:
	}

	// Submodules are not initialised; count them so the user can be warned.
	repo.Repo.Submodules, _ = gitexec.WithRepo(dst).SubmoduleCount()

	return nil
}
