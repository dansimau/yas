package workyard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/dansimau/yas/pkg/xexec"
	"github.com/hashicorp/go-multierror"
	"github.com/sourcegraph/conc/pool"
)

// Remove removes the yard and reports whether its directory went to the
// operating system's trash. Without force, nothing is removed if any worktree
// has uncommitted changes; locked worktrees need force twice (they are
// unlocked first). The directory is moved to the trash when a "trash" command
// is available (macOS has one built in), which takes a moment however big the
// yard is, and deleted in place otherwise. Git's records of the worktrees are
// pruned, the branches workyard created are deleted where they are fully
// merged (or unconditionally with force), and the metadata is removed.
func (y *Yard) Remove(ctx context.Context, o RemoveOptions) (bool, error) {
	if o.Log == nil {
		o.Log = io.Discard
	}

	if o.Force == 0 {
		dirty, err := y.dirtyRepos(ctx)
		if err != nil {
			return false, err
		}

		if len(dirty) > 0 {
			return false, fmt.Errorf("%w:\n  %s", ErrDirty, strings.Join(dirty, "\n  "))
		}
	}

	locked, err := y.lockedRepos()
	if err != nil {
		return false, err
	}

	if len(locked) > 0 && o.Force < 2 {
		paths := make([]string, len(locked))
		for i, repo := range locked {
			paths[i] = repo.Path
		}

		return false, fmt.Errorf("%w:\n  %s", ErrLocked, strings.Join(paths, "\n  "))
	}

	for _, repo := range locked {
		if err := gitexec.WithRepo(repo.Source).WorktreeUnlock(y.RepoDir(repo)); err != nil {
			return false, fmt.Errorf("%s: unlock: %w", repo.Path, err)
		}
	}

	for _, repo := range y.Meta.Repos {
		if !sourceExists(repo) {
			_, _ = fmt.Fprintf(o.Log, "warning: %s: source repository %s no longer exists; the directory is removed but git's record of the worktree cannot be pruned\n", repo.Path, repo.Source)
		}
	}

	root, err := realPath(y.Root)
	if err != nil {
		return false, err
	}

	trashed, err := trashOrRemove(root)
	if err != nil {
		return false, err
	}

	y.pruneParents(root)

	// Prune once per shared git directory, and delete the branches workyard
	// created. Without force only fully merged branches go; the rest are
	// reported, since deleting them would lose work.
	var (
		errs   error
		pruned = map[string]bool{}
	)

	for _, repo := range y.Meta.Repos {
		if !sourceExists(repo) {
			continue
		}

		git := gitexec.WithRepo(repo.Source)

		if !pruned[repo.CommonDir] {
			pruned[repo.CommonDir] = true

			if err := git.WorktreePrune(); err != nil {
				errs = multierror.Append(errs, fmt.Errorf("%s: prune: %w", repo.Path, err))
			}
		}

		if !repo.CreatedBranch {
			continue
		}

		if o.Force > 0 {
			if err := git.DeleteBranch(repo.Ref); err != nil {
				errs = multierror.Append(errs, fmt.Errorf("%s: delete branch %s: %w", repo.Path, repo.Ref, err))
			}

			continue
		}

		if err := git.DeleteBranchSafe(repo.Ref); err != nil {
			_, _ = fmt.Fprintf(o.Log, "warning: %s: branch %s was not deleted because it is not fully merged (hint: use --force to delete it anyway)\n", repo.Path, repo.Ref)
		}
	}

	if err := removeMetadata(y.Source, y.ID); err != nil {
		errs = multierror.Append(errs, err)
	}

	return trashed, errs
}

// trashOrRemove moves the directory root to the operating system's trash
// with the "trash" command, if there is one on the PATH, and reports whether
// it did. Whenever root is still there afterwards (no command, or it failed)
// the directory is deleted in place instead.
func trashOrRemove(root string) (bool, error) {
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}

	if _, err := exec.LookPath("trash"); err == nil {
		_ = xexec.Command("trash", root).WithStdout(nil).WithStderr(nil).Run()

		if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
	}

	return false, removeAll(root)
}

// pruneParents deletes the directories between the yards directory and the
// yard's (real) root that its removal left empty (e.g. "a" for a yard named
// "a/b"). Best effort: removing a non-empty directory fails, which is fine.
func (y *Yard) pruneParents(root string) {
	cfg, err := LoadConfig(y.Source)
	if err != nil {
		return
	}

	dir, err := cfg.yardsDir(y.Source)
	if err != nil {
		return
	}

	for parent := filepath.Dir(root); parent != dir && isWithin(dir, parent); parent = filepath.Dir(parent) {
		if os.Remove(parent) != nil {
			return
		}
	}
}

// RemoveOrphan removes a yard whose source directory (and with it the
// metadata) no longer exists, reporting whether it went to the trash.
// Nothing but the directory can be cleaned up.
func RemoveOrphan(root string) (bool, error) {
	if !isPointer(root) {
		return false, fmt.Errorf("%w: %s", ErrNotAWorkyard, root)
	}

	return trashOrRemove(root)
}

// dirtyRepos returns the paths of repositories with uncommitted changes or
// untracked files.
func (y *Yard) dirtyRepos(ctx context.Context) ([]string, error) {
	var (
		mu    sync.Mutex
		dirty []string
	)

	p := pool.New().WithMaxGoroutines(runtime.NumCPU()).WithErrors().WithContext(ctx)

	for _, repo := range y.Meta.Repos {
		p.Go(func(context.Context) error {
			dir := y.RepoDir(repo)
			if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
				return nil
			}

			// Without its source repository git cannot tell what changed; the
			// directory is deleted as-is.
			if !sourceExists(repo) {
				return nil
			}

			entries, err := gitexec.WithRepo(dir).StatusEntries()
			if err != nil {
				return fmt.Errorf("%s: %w", repo.Path, err)
			}

			if len(entries) > 0 {
				mu.Lock()

				dirty = append(dirty, repo.Path)

				mu.Unlock()
			}

			return nil
		})
	}

	if err := p.Wait(); err != nil {
		return nil, err
	}

	sort.Strings(dirty)

	return dirty, nil
}

// lockedRepos returns the repositories whose worktree is locked. Git records
// worktree paths as real paths, so the yard's are compared the same way.
func (y *Yard) lockedRepos() ([]Repo, error) {
	var (
		locked = map[string]bool{}
		listed = map[string]bool{}
	)

	for _, repo := range y.Meta.Repos {
		if listed[repo.CommonDir] || !sourceExists(repo) {
			continue
		}

		listed[repo.CommonDir] = true

		worktrees, err := gitexec.WithRepo(repo.Source).Worktrees()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", repo.Path, err)
		}

		for _, wt := range worktrees {
			if wt.Locked {
				locked[wt.Path] = true
			}
		}
	}

	var repos []Repo

	for _, repo := range y.Meta.Repos {
		dir, err := realPath(y.RepoDir(repo))
		if err != nil {
			return nil, err
		}

		if locked[dir] {
			repos = append(repos, repo)
		}
	}

	return repos, nil
}

func sourceExists(repo Repo) bool {
	_, err := os.Stat(repo.Source)
	if err != nil {
		return false
	}

	_, err = os.Stat(repo.CommonDir)

	return err == nil
}
