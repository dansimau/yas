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

	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/dansimau/yas/pkg/xexec"
	"github.com/sourcegraph/conc/pool"
)

// gitEnv is the environment fan-out git commands run in: cleaned of inherited
// GIT_* variables, and never prompting for credentials (there is no terminal
// to answer on when many repositories run at once).
func gitEnv() []string {
	return append(gitexec.CleanedGitEnv(), "GIT_TERMINAL_PROMPT=0")
}

// Run executes git with gitArgs in every repository of the yard concurrently
// and calls onResult with each outcome, in path order when o.Ordered is set
// and in completion order otherwise. onResult is always called from the
// calling goroutine.
func (y *Yard) Run(ctx context.Context, o RunOptions, gitArgs []string, onResult func(Result)) error {
	if o.Jobs <= 0 {
		o.Jobs = runtime.NumCPU() * 2
	}

	repos := y.Meta.Repos
	results := make([]chan Result, len(repos))
	completed := make(chan Result, len(repos))

	p := pool.New().WithMaxGoroutines(o.Jobs)

	for i, repo := range repos {
		results[i] = make(chan Result, 1)

		p.Go(func() {
			result := y.runOne(ctx, o, repo, gitArgs)
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

func (y *Yard) runOne(ctx context.Context, o RunOptions, repo Repo, gitArgs []string) Result {
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

	args := []string{"git", "--no-pager"}
	if o.Color {
		args = append(args, "-c", "color.ui=always")
	}

	args = append(args, "-C", dir)
	args = append(args, gitArgs...)

	var stdout, stderr bytes.Buffer

	err := xexec.CommandContext(ctx, args...).
		WithEnvVars(gitEnv()).
		WithStdin(nil).
		WithStdout(&stdout).
		WithStderr(&stderr).
		Run()

	result.Stdout = stdout.Bytes()
	result.Stderr = stderr.Bytes()

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
		WithEnvVars(gitEnv()).
		WithStdin(nil).
		WithStdout(nil).
		WithStderr(nil).
		Output()
	if err != nil || len(out) == 0 {
		return "HEAD"
	}

	return strings.TrimSpace(string(out))
}
