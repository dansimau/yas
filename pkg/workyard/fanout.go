package workyard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/dansimau/yas/pkg/xexec"
	"github.com/sourcegraph/conc/pool"
)

// Run executes command (a program and its arguments) in every repository of
// the yard concurrently, with the repository directory as the working
// directory and its output captured, and calls onResult with each outcome: in
// path order when o.Ordered is set and in completion order otherwise.
// onResult is always called from the calling goroutine.
//
// The command inherits the environment unchanged (with PWD set to the
// repository), exactly as if the user had run it there.
func (y *Yard) Run(ctx context.Context, o RunOptions, command []string, onResult func(Result)) error {
	if len(command) == 0 {
		return errors.New("no command given")
	}

	if o.Parallelism <= 0 {
		o.Parallelism = runtime.NumCPU() * 2
	}

	repos := y.Meta.Repos
	results := make([]chan Result, len(repos))
	completed := make(chan Result, len(repos))

	p := pool.New().WithMaxGoroutines(o.Parallelism)

	for i, repo := range repos {
		results[i] = make(chan Result, 1)

		p.Go(func() {
			var stdout, stderr bytes.Buffer

			result := y.runOne(ctx, repo, command, StdIO{Out: &stdout, Err: &stderr})
			result.Stdout = stdout.Bytes()
			result.Stderr = stderr.Bytes()

			results[i] <- result

			completed <- result
		})
	}

	if o.Ordered {
		for _, ch := range results {
			onResult(<-ch)
		}
	} else {
		for range repos {
			onResult(<-completed)
		}
	}

	p.Wait()

	return ctx.Err()
}

// RunSerial executes command in every repository of the yard one at a time,
// in path order, connected directly to stdio so that it can be interactive.
// onStart is called before the command runs in a repository (e.g. to print a
// header) and onResult after it has finished; Result.Stdout and Result.Stderr
// are always empty since nothing is captured.
func (y *Yard) RunSerial(ctx context.Context, stdio StdIO, command []string, onStart func(repo Repo, branch string), onResult func(Result)) error {
	if len(command) == 0 {
		return errors.New("no command given")
	}

	for _, repo := range y.Meta.Repos {
		if err := ctx.Err(); err != nil {
			return err
		}

		onStart(repo, currentBranch(ctx, y.RepoDir(repo)))
		onResult(y.runOne(ctx, repo, command, stdio))
	}

	return nil
}

func (y *Yard) runOne(ctx context.Context, repo Repo, command []string, stdio StdIO) Result {
	result := Result{Repo: repo}
	dir := y.RepoDir(repo)

	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			result.Err = fmt.Errorf("repository directory missing: %s", dir)
		} else {
			result.Err = err
		}

		return result
	}

	result.Branch = currentBranch(ctx, dir)

	// No explicit environment: the command inherits ours, and os/exec then
	// sets PWD to the working directory as a shell would.
	err := xexec.CommandContext(ctx, command...).
		WithWorkingDir(dir).
		WithStdin(stdio.In).
		WithStdout(stdio.Out).
		WithStderr(stdio.Err).
		Run()
	if err != nil {
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.Err = err
		}
	}

	return result
}

// currentBranch returns the branch checked out at dir, or "HEAD" when detached
// or unknown.
func currentBranch(ctx context.Context, dir string) string {
	out, err := xexec.CommandContext(ctx, "git", "-C", dir, "symbolic-ref", "--short", "-q", "HEAD").
		WithStdin(nil).
		WithStdout(nil).
		WithStderr(nil).
		Output()
	if err != nil || len(out) == 0 {
		return "HEAD"
	}

	return strings.TrimSpace(string(out))
}
