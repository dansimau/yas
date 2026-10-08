package workyard

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
)

// removeWorkers is how many directories removeAll deletes from at once. The
// deletion rate is bound by the filesystem, not the CPU: on APFS four to
// eight workers are fastest and thirty-two are slower than one.
var removeWorkers = min(runtime.NumCPU(), 8)

// removeAll deletes the directory tree (or single file) at path. Unlike
// os.RemoveAll it deletes from several directories at once, makes read-only
// directories (which a copied source tree may well contain) writable so that
// their entries can be unlinked, and does not mind entries that disappear
// meanwhile, so that two removals of the same tree can overlap. The first
// error is returned once everything that could be deleted has been.
func removeAll(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return ignoreNotExist(err)
	}

	if !info.IsDir() {
		return ignoreNotExist(os.Remove(path))
	}

	r := &remover{}
	r.cond = sync.NewCond(&r.mu)
	r.push(&dirJob{path: path})

	for range removeWorkers {
		go r.work()
	}

	r.wg.Wait()

	r.mu.Lock()
	r.done = true
	r.mu.Unlock()
	r.cond.Broadcast()

	return r.err
}

// remover deletes a tree with a pool of workers that each take one directory
// at a time: they unlink its files and queue its subdirectories. A directory
// is removed by whichever worker finishes its last subdirectory.
type remover struct {
	mu   sync.Mutex
	cond *sync.Cond
	// stack holds the directories waiting for a worker. Taking the most
	// recently queued one keeps the walk depth-first, so that directories
	// are finished (and removed) soon after they are opened.
	stack []*dirJob
	done  bool
	err   error
	// wg counts the directories queued but not yet processed.
	wg sync.WaitGroup
}

type dirJob struct {
	path   string
	parent *dirJob
	// pending is the number of subdirectories not yet removed, plus one while
	// the directory itself is still being listed.
	pending atomic.Int32
}

func (r *remover) push(j *dirJob) {
	r.wg.Add(1)

	r.mu.Lock()
	r.stack = append(r.stack, j)
	r.mu.Unlock()

	r.cond.Signal()
}

func (r *remover) fail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.err == nil {
		r.err = err
	}
}

func (r *remover) work() {
	for {
		r.mu.Lock()

		for len(r.stack) == 0 && !r.done {
			r.cond.Wait()
		}

		if r.done {
			r.mu.Unlock()

			return
		}

		j := r.stack[len(r.stack)-1]
		r.stack = r.stack[:len(r.stack)-1]
		r.mu.Unlock()

		r.process(j)
		r.wg.Done()
	}
}

func (r *remover) process(j *dirJob) {
	j.pending.Store(1)

	var entries []os.DirEntry

	err := withAccess(j.path, func() error {
		var err error

		entries, err = os.ReadDir(j.path)

		return ignoreNotExist(err)
	})
	if err != nil {
		r.fail(err)
	}

	for _, e := range entries {
		p := filepath.Join(j.path, e.Name())

		if e.IsDir() {
			j.pending.Add(1)
			r.push(&dirJob{path: p, parent: j})

			continue
		}

		if err := withAccess(j.path, func() error { return ignoreNotExist(os.Remove(p)) }); err != nil {
			r.fail(err)
		}
	}

	r.finish(j)
}

// finish records that j is done with, removes it when none of its
// subdirectories are left, and then does the same for its parent.
func (r *remover) finish(j *dirJob) {
	for ; j != nil && j.pending.Add(-1) == 0; j = j.parent {
		err := withAccess(filepath.Dir(j.path), func() error { return ignoreNotExist(os.Remove(j.path)) })
		if err != nil {
			r.fail(err)
		}
	}
}

// withAccess runs op and, when it fails for lack of permission, makes dir
// (the directory op needs to read, or to unlink from) accessible to its
// owner and tries once more.
func withAccess(dir string, op func() error) error {
	err := op()
	if !errors.Is(err, fs.ErrPermission) {
		return err
	}

	info, statErr := os.Lstat(dir)
	if statErr != nil || os.Chmod(dir, info.Mode().Perm()|0o700) != nil {
		return err
	}

	return op()
}

func ignoreNotExist(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	return err
}
