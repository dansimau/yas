package workyard

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
)

// entryKind classifies a directory entry found while scanning the source.
type entryKind int

const (
	// kindAncestor is a directory with at least one repository below it; it is
	// recreated and its children handled individually.
	kindAncestor entryKind = iota
	// kindSubtree is a directory with no repository below it; it is copied (or
	// cloned) as a whole.
	kindSubtree
	kindRepo
	kindFile
	kindSymlink
	// kindOther is a socket, FIFO or device, which is skipped.
	kindOther
)

// entry is one directory entry in the scanned tree.
type entry struct {
	Rel  string
	Info fs.FileInfo
	Kind entryKind
	// Dir is set for kindAncestor.
	Dir *dirNode
	// Repo is set for kindRepo.
	Repo *RepoPlan
}

// dirNode is a directory that must be recreated because a repository lives
// somewhere below it.
type dirNode struct {
	Rel      string
	Info     fs.FileInfo
	Children []*entry
}

// Plan is the result of scanning a source directory: what will be copied and
// how each repository will be checked out.
type Plan struct {
	Source string
	Target string
	Branch string
	// Root is the source directory itself, or nil when the source is a
	// repository (Repos then has a single entry at ".").
	Root  *dirNode
	Repos []*RepoPlan
	// Subtrees and Files count the directories copied whole and the
	// individual files and symlinks copied.
	Subtrees int
	Files    int
}

// Describe writes a human-readable summary of the plan.
func (p *Plan) Describe(w io.Writer) {
	_, _ = fmt.Fprintf(w, "source: %s\ntarget: %s\nbranch: %s\n\n", p.Source, p.Target, p.Branch)

	if p.Root != nil {
		_, _ = fmt.Fprintf(w, "copy: %d subtree(s), %d file(s)\n", p.Subtrees, p.Files)
		describeDir(w, p.Root)
		_, _ = fmt.Fprintln(w)
	}

	_, _ = fmt.Fprintf(w, "repositories: %d\n", len(p.Repos))

	for _, repo := range p.Repos {
		_, _ = fmt.Fprintf(w, "  %s: %s\n", repo.Repo.Path, repo.Describe())
	}
}

func describeDir(w io.Writer, dir *dirNode) {
	for _, child := range dir.Children {
		switch child.Kind {
		case kindSubtree:
			_, _ = fmt.Fprintf(w, "  %s/ (subtree)\n", child.Rel)
		case kindAncestor:
			describeDir(w, child.Dir)
		case kindRepo, kindFile, kindSymlink, kindOther:
		}
	}
}

// Failed returns the repositories whose branch could not be resolved.
func (p *Plan) Failed() []*RepoPlan {
	var failed []*RepoPlan

	for _, repo := range p.Repos {
		if repo.Err != nil {
			failed = append(failed, repo)
		}
	}

	return failed
}

// isRepoDir reports whether a directory with the given entry names is a git
// repository: a working tree (with a .git directory or file) or a bare
// repository.
func isRepoDir(names map[string]fs.DirEntry) bool {
	if _, ok := names[".git"]; ok {
		return true
	}

	head, hasHead := names["HEAD"]
	objects, hasObjects := names["objects"]
	refs, hasRefs := names["refs"]

	return hasHead && hasObjects && hasRefs &&
		!head.IsDir() && objects.IsDir() && refs.IsDir()
}

type scanner struct {
	source string
	// slots bounds the number of goroutines scanning directories: a child
	// directory is scanned in its own goroutine only when a slot is free, and
	// inline otherwise, so the walk stays depth-first and never holds more
	// than a few directories' worth of entries in memory.
	slots chan struct{}

	mu       sync.Mutex
	repos    []*RepoPlan
	subtrees int
	files    int
}

// Scan walks source (never following symlinks, never descending into
// repositories) and returns the copy plan. Repository branch actions are not
// resolved; see Plan.
func Scan(ctx context.Context, source string) (*Plan, error) {
	info, err := os.Lstat(source)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("source %s is not a directory", source)
	}

	s := &scanner{
		source: source,
		slots:  make(chan struct{}, runtime.NumCPU()),
	}

	root, rootRepo, err := s.scanDir(ctx, ".", info)
	if err != nil {
		return nil, err
	}

	sort.Slice(s.repos, func(i, j int) bool { return s.repos[i].Repo.Path < s.repos[j].Repo.Path })

	plan := &Plan{
		Source:   source,
		Repos:    s.repos,
		Subtrees: s.subtrees,
		Files:    s.files,
	}

	if rootRepo == nil {
		plan.Root = root
	}

	return plan, nil
}

// scanDir reads the directory at rel. It returns a dirNode when the directory
// is an ancestor of a repository, a RepoPlan when the directory is itself a
// repository, and neither when nothing below it is a repository (so it can be
// copied as one unit).
func (s *scanner) scanDir(ctx context.Context, rel string, info fs.FileInfo) (*dirNode, *RepoPlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	entries, err := os.ReadDir(filepath.Join(s.source, rel))
	if err != nil {
		return nil, nil, err
	}

	names := make(map[string]fs.DirEntry, len(entries))
	for _, e := range entries {
		names[e.Name()] = e
	}

	if isRepoDir(names) {
		repo := &RepoPlan{Repo: Repo{
			Path:   rel,
			Source: filepath.Join(s.source, rel),
		}}

		s.mu.Lock()
		s.repos = append(s.repos, repo)
		s.mu.Unlock()

		return nil, repo, nil
	}

	children := make([]*entry, len(entries))
	errs := make([]error, len(entries))

	var wg sync.WaitGroup

	for i, e := range entries {
		if rel == "." && e.Name() == workyardDir {
			// The source's own .workyard directory (config and yard metadata)
			// is never copied: a yard only gets a pointer back to the source.
			continue
		}

		childRel := filepath.Join(rel, e.Name())

		childInfo, err := e.Info()
		if err != nil {
			return nil, nil, err
		}

		child := &entry{Rel: childRel, Info: childInfo}
		children[i] = child

		if !childInfo.IsDir() {
			switch {
			case childInfo.Mode()&fs.ModeSymlink != 0:
				child.Kind = kindSymlink
			case childInfo.Mode().IsRegular():
				child.Kind = kindFile
			default:
				child.Kind = kindOther
			}

			continue
		}

		child.Kind = kindSubtree

		scan := func() {
			dir, repo, err := s.scanDir(ctx, childRel, childInfo)
			errs[i] = err

			switch {
			case err != nil:
			case dir != nil:
				child.Kind = kindAncestor
				child.Dir = dir
			case repo != nil:
				child.Kind = kindRepo
				child.Repo = repo
			}
		}

		select {
		case s.slots <- struct{}{}:
			wg.Add(1)

			go func() {
				defer wg.Done()
				defer func() { <-s.slots }()

				scan()
			}()
		default:
			scan()
		}
	}

	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return nil, nil, err
		}
	}

	hasRepo := false

	for _, child := range children {
		if child != nil && (child.Kind == kindRepo || child.Kind == kindAncestor) {
			hasRepo = true
		}
	}

	// The source root is always recreated entry by entry (so metadata can be
	// written into it); any other directory without a repository below it is
	// copied as one unit by the caller.
	if !hasRepo && rel != "." {
		return nil, nil, nil
	}

	node := &dirNode{Rel: rel, Info: info}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, child := range children {
		if child == nil {
			continue
		}

		switch child.Kind {
		case kindSubtree:
			s.subtrees++
		case kindFile, kindSymlink:
			s.files++
		case kindRepo, kindAncestor, kindOther:
		}

		node.Children = append(node.Children, child)
	}

	return node, nil, nil
}
