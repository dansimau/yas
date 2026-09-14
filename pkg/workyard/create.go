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

// ErrUnresolvedBranch is returned by Create when the branch cannot be checked
// out in one or more repositories; nothing is created in that case.
var ErrUnresolvedBranch = errors.New("cannot resolve branch in some repositories")

// Created is the outcome of a successful Create.
type Created struct {
	Yard *Yard
	// Cloned and Copied count the units (subtrees and files) that were cloned
	// and copied respectively; Skipped counts special files that were not
	// copied.
	Cloned  int
	Copied  int
	Skipped int
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
	if o.Jobs <= 0 {
		o.Jobs = runtime.NumCPU() * 4
	}

	if o.GitJobs <= 0 {
		o.GitJobs = runtime.NumCPU()
	}

	if o.Log == nil {
		o.Log = io.Discard
	}
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

	target, err := realPath(o.Target)
	if err != nil {
		return nil, err
	}

	if isWithin(source, target) || isWithin(target, source) {
		return nil, fmt.Errorf("target %s and source %s must not contain each other", target, source)
	}

	if entries, err := os.ReadDir(target); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrTargetNotEmpty, target)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("target: %w", err)
	}

	if !o.AllowNested {
		root, err := findRoot(source)
		if err != nil {
			return nil, err
		}

		if root != "" {
			return nil, fmt.Errorf("source is inside the workyard %s (hint: use --allow-nested to copy a workyard)", root)
		}
	}

	// The source may be a repository root, but not a directory inside a
	// repository's working tree: that would copy files git tracks without
	// making them a worktree.
	if top, err := gitexec.WithRepo(source).TopLevel(); err == nil {
		realTop, err := filepath.EvalSymlinks(top)
		if err != nil {
			return nil, err
		}

		if realTop != source {
			return nil, fmt.Errorf("source %s is inside the git repository %s (hint: use the repository root or a directory outside it)", source, realTop)
		}
	}

	branch := o.Branch
	if branch == "" {
		branch = filepath.Base(target)
	}

	if err := gitexec.WithRepo(source).CheckBranchName(branch); err != nil {
		return nil, err
	}

	cfg, err := LoadConfig(source)
	if err != nil {
		return nil, err
	}

	plan, err := Scan(ctx, source)
	if err != nil {
		return nil, err
	}

	plan.Target = target
	plan.Branch = branch

	resolveRefs(ctx, plan, cfg, o.Detach, o.GitJobs)

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
		copier: newCopier(o.CopyMode, func(msg string) { _, _ = fmt.Fprintln(o.Log, "warning: "+msg) }),
		groups: map[string]*sync.Mutex{},
	}

	if err := c.run(ctx); err != nil {
		return nil, err
	}

	return &Created{
		Yard:    &Yard{Root: plan.Target, Meta: c.meta},
		Cloned:  int(c.copier.cloned.Load()),
		Copied:  int(c.copier.copied.Load()),
		Skipped: int(c.copier.skipped.Load()),
		Elapsed: time.Since(start),
	}, nil
}

type creator struct {
	o      CreateOptions
	plan   *Plan
	copier *copier

	createdTarget bool
	// rootIsRepo is true when the source itself is a repository, so the target
	// is a single worktree and metadata can only be written after it exists.
	rootIsRepo bool

	metaMu sync.Mutex
	meta   Metadata
	// added lists the worktrees created so far, for rollback.
	added []*RepoPlan
	// groups serializes worktree operations on repositories that share a git
	// directory, which would otherwise race on .git/config.
	groups map[string]*sync.Mutex
}

func (c *creator) run(ctx context.Context) error {
	target := c.plan.Target

	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return err
		}

		c.createdTarget = true
	}

	c.rootIsRepo = c.plan.Root == nil
	c.meta = c.initialMetadata()

	if !c.rootIsRepo {
		if err := writeMetadata(target, c.meta); err != nil {
			return c.fail(err)
		}
	}

	if c.plan.Root != nil {
		if err := c.copyFiles(ctx); err != nil {
			return c.fail(err)
		}
	}

	if err := c.addWorktrees(ctx); err != nil {
		return c.fail(err)
	}

	if err := c.copyConfig(); err != nil {
		return c.fail(err)
	}

	c.meta.Complete = true

	if err := writeMetadata(target, c.meta); err != nil {
		return c.fail(err)
	}

	return nil
}

func (c *creator) initialMetadata() Metadata {
	meta := Metadata{
		Version:    MetadataVersion,
		CreatedAt:  time.Now().UTC(),
		YasVersion: buildVersion(),
		Source:     c.plan.Source,
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

// copyFiles recreates the ancestor directories and copies everything that is
// not a repository.
func (c *creator) copyFiles(ctx context.Context) error {
	var (
		ancestors []*dirNode
		leaves    []*entry
	)

	// Ancestor directories are created top-down before their contents are
	// copied, and have their final mode and mtime applied bottom-up after.
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
		return err
	}

	p := pool.New().WithMaxGoroutines(c.o.Jobs).WithErrors().WithContext(ctx)

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
		return err
	}

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
	if len(c.plan.Repos) == 0 {
		return nil
	}

	var runner Runner
	if c.o.NewRunner != nil {
		runner = c.o.NewRunner(c.o.GitJobs, "Creating worktrees")
	} else {
		runner = NewSilentRunner(c.o.GitJobs)
	}

	var (
		errsMu sync.Mutex
		errs   error
	)

	for i, repo := range c.plan.Repos {
		runner.Add(repo.Repo.Path, func() error {
			err := c.addWorktree(ctx, i, repo)
			if err != nil {
				errsMu.Lock()

				errs = multierror.Append(errs, fmt.Errorf("%s: %w", repo.Repo.Path, err))

				errsMu.Unlock()
			}

			return err
		})
	}

	// The runner reports the same per-task errors we collect above, and adds
	// its own display; the collected errors are the ones we return.
	_ = runner.Start(true)

	return errs
}

func (c *creator) addWorktree(ctx context.Context, index int, repo *RepoPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	mu := c.groupMutex(repo.Repo.CommonDir)

	mu.Lock()
	defer mu.Unlock()

	if err := addWorktree(repo, c.dst(repo.Repo.Path)); err != nil {
		return err
	}

	c.metaMu.Lock()
	defer c.metaMu.Unlock()

	c.added = append(c.added, repo)
	c.meta.Repos[index] = repo.Repo

	if c.rootIsRepo {
		return nil
	}

	return writeMetadata(c.plan.Target, c.meta)
}

// copyConfig copies the source's .workyard/config.yaml, if any, so the yard
// can itself be used as a source.
func (c *creator) copyConfig() error {
	src := filepath.Join(c.plan.Source, workyardDir, configFile)

	info, err := os.Lstat(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return err
	}

	if err := os.MkdirAll(filepath.Join(c.plan.Target, workyardDir), 0o755); err != nil {
		return err
	}

	return c.copier.copyFile(src, filepath.Join(c.plan.Target, workyardDir, configFile), info)
}

// fail handles a failure during creation: it keeps the partial yard (marked
// incomplete) when requested, otherwise rolls everything back.
func (c *creator) fail(cause error) error {
	if c.o.KeepPartial {
		c.meta.Complete = false

		if err := writeMetadata(c.plan.Target, c.meta); err != nil {
			cause = multierror.Append(cause, err)
		}

		_, _ = fmt.Fprintf(c.o.Log, "keeping partial workyard at %s\n", c.plan.Target)

		return fmt.Errorf("%w: %w", ErrPartialFailure, cause)
	}

	_, _ = fmt.Fprintf(c.o.Log, "rolling back %s\n", c.plan.Target)

	if err := c.rollback(); err != nil {
		cause = multierror.Append(cause, fmt.Errorf("rollback: %w", err))
	}

	return fmt.Errorf("%w: %w", ErrPartialFailure, cause)
}

func (c *creator) rollback() error {
	var errs error

	for _, repo := range c.added {
		dst := c.dst(repo.Repo.Path)
		if err := gitexec.WithRepo(repo.Repo.Source).WorktreeRemoveForce(dst, 2); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if c.createdTarget {
		if err := removeAll(c.plan.Target); err != nil {
			errs = multierror.Append(errs, err)
		}

		return errs
	}

	// The target existed (empty) before: empty it again but leave it in place.
	entries, err := os.ReadDir(c.plan.Target)
	if err != nil {
		return multierror.Append(errs, err)
	}

	for _, e := range entries {
		if err := removeAll(filepath.Join(c.plan.Target, e.Name())); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	return errs
}

// silentRunner runs tasks concurrently with no output.
type silentRunner struct {
	jobs  int
	tasks []func() error
}

// NewSilentRunner returns a Runner that runs up to jobs tasks concurrently and
// produces no output.
func NewSilentRunner(jobs int) Runner {
	return &silentRunner{jobs: jobs}
}

func (r *silentRunner) Add(_ string, fn func() error) {
	r.tasks = append(r.tasks, fn)
}

func (r *silentRunner) Start(bool) error {
	p := pool.New().WithMaxGoroutines(r.jobs).WithErrors()

	for _, task := range r.tasks {
		p.Go(task)
	}

	return p.Wait()
}
