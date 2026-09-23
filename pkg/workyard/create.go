package workyard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/hashicorp/go-multierror"
	"github.com/sourcegraph/conc/pool"
)

// Created is the outcome of a successful Create.
type Created struct {
	Yard    *Yard
	Elapsed time.Duration
}

// realPath returns the absolute path of p with symlinks resolved. When p does
// not exist, its longest existing ancestor is resolved instead, so the result
// is where p would be created.
func realPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}

	existing := abs
	rest := ""

	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}

		parent := filepath.Dir(existing)
		if parent == existing {
			return abs, nil
		}

		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}

	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}

	return filepath.Join(resolved, rest), nil
}

// isWithin reports whether child is parent or inside parent.
func isWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}

	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, "../"))
}

func (o *CreateOptions) applyDefaults() {
	if o.Parallelism <= 0 {
		o.Parallelism = runtime.NumCPU()
	}

	if o.Log == nil {
		o.Log = io.Discard
	}
}

// isGitRepo reports whether dir is a git repository (working tree or bare) or
// inside one's working tree.
func isGitRepo(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}

	names := make(map[string]os.DirEntry, len(entries))
	for _, e := range entries {
		names[e.Name()] = e
	}

	if isRepoDir(names) {
		return true, nil
	}

	_, err = gitexec.WithRepo(dir).TopLevel()

	return err == nil, nil
}

// resolveTarget returns the real path of the directory to create: o.Target,
// or else o.Name under the configured yards directory.
func resolveTarget(o CreateOptions, source string, cfg Config) (string, error) {
	if o.Target != "" {
		return realPath(o.Target)
	}

	if o.Name == "" {
		return "", ErrNoName
	}

	if !filepath.IsLocal(o.Name) {
		return "", fmt.Errorf("%w: %q", ErrInvalidName, o.Name)
	}

	dir, err := cfg.yardsDir(source)
	if err != nil {
		return "", err
	}

	return realPath(filepath.Join(dir, o.Name))
}

// PlanCreate validates the options, scans the source and decides how every
// repository will be checked out, without touching the target.
func PlanCreate(ctx context.Context, o CreateOptions) (*Plan, error) {
	o.applyDefaults()

	source, err := realPath(o.Source)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(source)
	if err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("source %s is not a directory", source)
	}

	// A workyard's root is by definition not a repository: worktrees of a
	// single repository are what git worktree is for.
	inRepo, err := isGitRepo(source)
	if err != nil {
		return nil, err
	}

	if inRepo {
		return nil, ErrSourceIsRepo
	}

	// Nor is it another workyard: yards point back to a plain source.
	root, err := findRoot(source)
	if err != nil {
		return nil, err
	}

	if root != "" {
		return nil, fmt.Errorf("%w: %s", ErrNestedWorkyard, root)
	}

	cfg, err := LoadConfig(source)
	if err != nil {
		return nil, err
	}

	target, err := resolveTarget(o, source, cfg)
	if err != nil {
		return nil, err
	}

	// The target and the source must not contain each other, with one
	// exception: a yard may live inside the source's .workyard directory
	// (which is never copied), as long as it does not swallow the yard
	// metadata kept in .workyard/yards.
	metaDir := filepath.Join(source, workyardDir, yardsDir)
	if isWithin(target, metaDir) || (isWithin(source, target) && !isWithin(filepath.Join(source, workyardDir), target)) {
		return nil, fmt.Errorf("destination %s and source %s must not contain each other (a yard may live under %s, but not at %s)",
			target, source, filepath.Join(source, workyardDir), metaDir)
	}

	if entries, err := os.ReadDir(target); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrTargetNotEmpty, target)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("destination: %w", err)
	}

	branch := o.Branch

	switch {
	case branch != "":
	case o.Name != "":
		branch = o.Name
	default:
		branch = filepath.Base(target)
	}

	if err := gitexec.WithRepo(source).CheckBranchName(branch); err != nil {
		return nil, err
	}

	plan, err := Scan(ctx, source)
	if err != nil {
		return nil, err
	}

	plan.Target = target
	plan.Branch = branch

	resolveRefs(ctx, plan, cfg, o.Parallelism)

	return plan, nil
}

// Create builds a workyard of o.Source at o.Target.
func Create(ctx context.Context, o CreateOptions) (*Created, error) {
	start := time.Now()

	o.applyDefaults()

	plan, err := PlanCreate(ctx, o)
	if err != nil {
		return nil, err
	}

	if failed := plan.Failed(); len(failed) > 0 {
		err := ErrUnresolvedBranch
		for _, repo := range failed {
			err = multierror.Append(err, fmt.Errorf("%s: %w", repo.Repo.Path, repo.Err))
		}

		return nil, err
	}

	c := &creator{
		o:      o,
		plan:   plan,
		copier: newCopier(copyAuto, func(msg string) { _, _ = fmt.Fprintln(o.Log, "warning: "+msg) }),
		groups: map[string]*sync.Mutex{},
	}

	if err := c.run(ctx); err != nil {
		return nil, err
	}

	return &Created{
		Yard:    &Yard{Root: plan.Target, Source: plan.Source, ID: c.meta.ID, Meta: c.meta},
		Elapsed: time.Since(start),
	}, nil
}

type creator struct {
	o      CreateOptions
	plan   *Plan
	copier *copier

	metaMu sync.Mutex
	meta   Metadata
	// rollback undoes everything done so far when a later step fails.
	rollback rollback
	// groups serializes worktree operations on repositories that share a git
	// directory, which would otherwise race on .git/config.
	groups map[string]*sync.Mutex
}

func (c *creator) run(ctx context.Context) error {
	if err := c.prepareTarget(); err != nil {
		return c.fail(err)
	}

	ancestors, err := c.copyFiles(ctx)
	if err != nil {
		return c.fail(err)
	}

	if err := c.addWorktrees(ctx); err != nil {
		return c.fail(err)
	}

	// Ancestor directories were created writable so that files and worktrees
	// could be added underneath; only now do they get their source modes.
	if err := c.finishDirs(ancestors); err != nil {
		return c.fail(err)
	}

	c.meta.Complete = true

	if err := writeMetadata(c.meta); err != nil {
		return c.fail(err)
	}

	return nil
}

// prepareTarget creates the target directory, the pointer file in it and the
// initial (incomplete) metadata in the source, registering the rollback of
// each.
func (c *creator) prepareTarget() error {
	target := c.plan.Target

	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		// Undoing removes the missing ancestors created along with it, too
		// (e.g. the yards directory, or "a" for a yard named "a/b").
		created := target
		for parent := filepath.Dir(created); parent != created; parent = filepath.Dir(created) {
			if _, err := os.Lstat(parent); err == nil {
				break
			}

			created = parent
		}

		if err := os.MkdirAll(target, 0o755); err != nil {
			return err
		}

		c.rollback.add("remove "+created, func() error { return removeAll(created) })
	} else {
		// The target existed (empty) before: empty it again but leave it in
		// place.
		c.rollback.add("empty "+target, func() error {
			entries, err := os.ReadDir(target)
			if err != nil {
				return err
			}

			for _, e := range entries {
				if err := removeAll(filepath.Join(target, e.Name())); err != nil {
					return err
				}
			}

			return nil
		})
	}

	c.meta = c.initialMetadata()

	if err := writeMetadata(c.meta); err != nil {
		return err
	}

	c.rollback.add("remove metadata "+metadataPath(c.meta.Source, c.meta.ID), func() error {
		return removeMetadata(c.meta.Source, c.meta.ID)
	})

	return writePointer(target, c.plan.Source, c.meta.ID)
}

func (c *creator) initialMetadata() Metadata {
	meta := Metadata{
		Version:    MetadataVersion,
		ID:         yardID(c.plan.Target),
		CreatedAt:  time.Now().UTC(),
		YasVersion: buildVersion(),
		Source:     c.plan.Source,
		Target:     c.plan.Target,
		Branch:     c.plan.Branch,
	}

	if v, err := gitexec.WithRepo(c.plan.Source).GitVersion(); err == nil {
		meta.GitVersion = v.String()
	}

	for _, repo := range c.plan.Repos {
		meta.Repos = append(meta.Repos, repo.Repo)
	}

	return meta
}

func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}

	return "(devel)"
}

// copyFiles recreates the ancestor directories (writable, for now) and copies
// everything that is not a repository. It returns the ancestors so their
// modes can be applied once the worktrees exist.
func (c *creator) copyFiles(ctx context.Context) ([]*dirNode, error) {
	var (
		ancestors []*dirNode
		leaves    []*entry
	)

	var collect func(dir *dirNode) error

	collect = func(dir *dirNode) error {
		if dir.Rel != "." {
			if err := os.Mkdir(c.dst(dir.Rel), dir.Info.Mode().Perm()|0o700); err != nil {
				return err
			}
		}

		ancestors = append(ancestors, dir)

		for _, child := range dir.Children {
			if child.Kind == kindAncestor {
				if err := collect(child.Dir); err != nil {
					return err
				}

				continue
			}

			if child.Kind != kindRepo {
				leaves = append(leaves, child)
			}
		}

		return nil
	}

	if err := collect(c.plan.Root); err != nil {
		return nil, err
	}

	p := pool.New().WithMaxGoroutines(c.o.Parallelism * 4).WithErrors().WithContext(ctx)

	for _, leaf := range leaves {
		p.Go(func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}

			src := filepath.Join(c.plan.Source, leaf.Rel)
			dst := c.dst(leaf.Rel)

			if leaf.Kind == kindSubtree {
				return c.copier.CopyTree(src, dst, leaf.Info)
			}

			return c.copier.CopyEntry(src, dst, leaf.Info)
		})
	}

	if err := p.Wait(); err != nil {
		return nil, err
	}

	return ancestors, nil
}

// finishDirs applies the source modes (and, below the root, mtimes) to the
// ancestor directories, deepest first.
func (c *creator) finishDirs(ancestors []*dirNode) error {
	for i := len(ancestors) - 1; i >= 0; i-- {
		dir := ancestors[i]
		if dir.Rel == "." {
			// The root keeps its own mtime (it is about to change anyway) but
			// takes the source's mode.
			if err := os.Chmod(c.plan.Target, dir.Info.Mode().Perm()); err != nil {
				return err
			}

			continue
		}

		if err := c.copier.finishDir(c.dst(dir.Rel), dir.Info); err != nil {
			return err
		}
	}

	return nil
}

func (c *creator) dst(rel string) string {
	return filepath.Join(c.plan.Target, rel)
}

func (c *creator) groupMutex(commonDir string) *sync.Mutex {
	c.metaMu.Lock()
	defer c.metaMu.Unlock()

	mu, ok := c.groups[commonDir]
	if !ok {
		mu = &sync.Mutex{}
		c.groups[commonDir] = mu
	}

	return mu
}

// addWorktrees creates a worktree for every repository, recording each in
// the metadata as it lands.
func (c *creator) addWorktrees(ctx context.Context) error {
	var (
		errsMu sync.Mutex
		errs   error
	)

	p := pool.New().WithMaxGoroutines(c.o.Parallelism)

	for i, repo := range c.plan.Repos {
		p.Go(func() {
			if err := c.addWorktree(ctx, i, repo); err != nil {
				errsMu.Lock()

				errs = multierror.Append(errs, fmt.Errorf("%s: %w", repo.Repo.Path, err))

				errsMu.Unlock()
			}
		})
	}

	p.Wait()

	return errs
}

func (c *creator) addWorktree(ctx context.Context, index int, repo *RepoPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	mu := c.groupMutex(repo.Repo.CommonDir)

	mu.Lock()
	defer mu.Unlock()

	dst := c.dst(repo.Repo.Path)

	if err := addWorktree(repo, dst); err != nil {
		return err
	}

	c.metaMu.Lock()
	defer c.metaMu.Unlock()

	c.rollback.add("remove worktree "+dst, func() error {
		mu.Lock()
		defer mu.Unlock()

		git := gitexec.WithRepo(repo.Repo.Source)
		if err := git.WorktreeRemoveForce(dst, 2); err != nil {
			return err
		}

		// A branch workyard created would otherwise look pre-existing to the
		// next attempt and never be cleaned up.
		if repo.Repo.CreatedBranch {
			return git.DeleteBranch(repo.Repo.Ref)
		}

		return nil
	})

	c.meta.Repos[index] = repo.Repo

	return writeMetadata(c.meta)
}

// fail rolls back everything done so far and returns the combined error.
func (c *creator) fail(cause error) error {
	err := fmt.Errorf("%w: %w", ErrPartialFailure, cause)

	if rollbackErr := c.rollback.run(); rollbackErr != nil {
		return fmt.Errorf("%w\n%w", err, rollbackErr)
	}

	return err
}

// rollback is a list of undo operations, run in reverse order of registration.
type rollback struct {
	mu    sync.Mutex
	steps []rollbackStep
}

type rollbackStep struct {
	desc string
	fn   func() error
}

func (r *rollback) add(desc string, fn func() error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.steps = append(r.steps, rollbackStep{desc: desc, fn: fn})
}

// run executes every step, most recent first, and reports all failures
// together.
func (r *rollback) run() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var failures []string

	for i := len(r.steps) - 1; i >= 0; i-- {
		if err := r.steps[i].fn(); err != nil {
			failures = append(failures, fmt.Sprintf("* %s: %v", r.steps[i].desc, err))
		}
	}

	r.steps = nil

	if len(failures) == 0 {
		return nil
	}

	return errors.New("Errors while rolling back:\n" + strings.Join(failures, "\n"))
}
