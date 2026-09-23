package workyard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/hashicorp/go-multierror"
	"github.com/sourcegraph/conc/pool"
)

// Remove deletes the yard: every worktree is removed through its source
// repository (so git's worktree metadata is cleaned up), the directory is
// deleted, and the branches workyard created are deleted where they are fully
// merged (or unconditionally with force). Without force, nothing is removed
// if any worktree has uncommitted changes.
func (y *Yard) Remove(ctx context.Context, o RemoveOptions) error {
	if o.Log == nil {
		o.Log = io.Discard
	}

	if o.Force == 0 {
		dirty, err := y.dirtyRepos(ctx)
		if err != nil {
			return err
		}

		if len(dirty) > 0 {
			return fmt.Errorf("%w:\n  %s", ErrDirty, strings.Join(dirty, "\n  "))
		}
	}

	groups := map[string]*sync.Mutex{}
	for _, repo := range y.Meta.Repos {
		if _, ok := groups[repo.CommonDir]; !ok {
			groups[repo.CommonDir] = &sync.Mutex{}
		}
	}

	var (
		logMu sync.Mutex
		errs  error
	)

	p := pool.New().WithMaxGoroutines(runtime.NumCPU()).WithErrors().WithContext(ctx)

	for _, repo := range y.Meta.Repos {
		p.Go(func(context.Context) error {
			mu := groups[repo.CommonDir]

			mu.Lock()
			defer mu.Unlock()

			warning, err := y.removeWorktree(repo, o.Force)

			logMu.Lock()
			defer logMu.Unlock()

			if warning != "" {
				_, _ = fmt.Fprintln(o.Log, "warning: "+warning)
			}

			if err != nil {
				errs = multierror.Append(errs, fmt.Errorf("%s: %w", repo.Path, err))
			}

			return nil
		})
	}

	if err := p.Wait(); err != nil {
		return err
	}

	if errs != nil {
		return errs
	}

	if err := removeAll(y.Root); err != nil {
		return err
	}

	y.pruneParents()

	// Prune once per shared git directory, and delete the branches workyard
	// created. Without force only fully merged branches go; the rest are
	// reported, since deleting them would lose work.
	pruned := map[string]bool{}

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

	return errs
}

// pruneParents deletes the directories between the yards directory and the
// yard's root that its removal left empty (e.g. "a" for a yard named "a/b").
// Best effort: removing a non-empty directory fails, which is fine.
func (y *Yard) pruneParents() {
	cfg, err := LoadConfig(y.Source)
	if err != nil {
		return
	}

	dir, err := cfg.yardsDir(y.Source)
	if err != nil {
		return
	}

	root, err := realPath(y.Root)
	if err != nil {
		return
	}

	for parent := filepath.Dir(root); parent != dir && isWithin(dir, parent); parent = filepath.Dir(parent) {
		if os.Remove(parent) != nil {
			return
		}
	}
}

// RemoveOrphan deletes a yard whose source directory (and with it the
// metadata) no longer exists. Nothing but the directory can be cleaned up.
func RemoveOrphan(root string) error {
	if !isPointer(root) {
		return fmt.Errorf("%w: %s", ErrNotAWorkyard, root)
	}

	return removeAll(root)
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

func sourceExists(repo Repo) bool {
	_, err := os.Stat(repo.Source)
	if err != nil {
		return false
	}

	_, err = os.Stat(repo.CommonDir)

	return err == nil
}

// removeWorktree removes one worktree, returning a warning when it had to fall
// back to deleting the directory because the source repository is gone.
func (y *Yard) removeWorktree(repo Repo, force int) (string, error) {
	dir := y.RepoDir(repo)

	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return "", nil
	}

	if !sourceExists(repo) {
		if err := removeAll(dir); err != nil {
			return "", err
		}

		return fmt.Sprintf("%s: source repository %s no longer exists; removed the directory but could not prune its git metadata", repo.Path, repo.Source), nil
	}

	return "", gitexec.WithRepo(repo.Source).WorktreeRemoveForce(dir, force)
}

// removeAll deletes a directory tree, including read-only directories (whose
// entries cannot otherwise be unlinked), which a copied source tree may well
// contain.
func removeAll(path string) error {
	err := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil //nolint:nilerr // unreadable entries are left to RemoveAll to report
		}

		info, err := d.Info()
		if err != nil {
			return nil //nolint:nilerr // see above
		}

		if info.Mode().Perm()&0o700 != 0o700 {
			_ = os.Chmod(p, info.Mode().Perm()|0o700)
		}

		return nil
	})
	if err != nil {
		return err
	}

	return os.RemoveAll(path)
}
