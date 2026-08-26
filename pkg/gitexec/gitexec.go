// Package gitexec provides utilities for executing git commands.
package gitexec

import (
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"

	"github.com/dansimau/yas/pkg/xexec"
	"github.com/hashicorp/go-version"
)

var ErrDetachedHead = errors.New("currently in detached state")

// configWriteMu serializes writes to .git/config. Git takes the config lock
// without retrying, so concurrent writers just fail with "could not lock config
// file" (measured at ~10% with ten writers, ~25% for `git push -u` with five).
// yas pushes and refreshes branches in parallel, so tracking writes take turns.
// This only covers writes from this process; a concurrent yas elsewhere can
// still lose the race, exactly as two concurrent git commands would.
var configWriteMu sync.Mutex

const defaultRemoteName = "origin"

// isExitCode reports whether err is a command that exited with the given code.
func isExitCode(err error, code int) bool {
	exitErr := &exec.ExitError{}

	return errors.As(err, &exitErr) && exitErr.ExitCode() == code
}

type CloneOptions struct {
	URL   string
	Depth int
}

func Clone(path string, options CloneOptions) error {
	cmd := []string{"git", "clone", options.URL}
	if options.Depth != 0 {
		cmd = append(cmd, "--depth", "1", "-q")
	}

	cmd = append(cmd, path)

	return xexec.Command(cmd...).
		WithEnvVars(CleanedGitEnv()).
		WithStdout(nil).Run()
}

type Repo struct {
	path string
}

func WithRepo(path string) *Repo {
	return &Repo{path: path}
}

func (r *Repo) run(args ...string) error {
	_, err := r.output(args...)

	return err
}

func (r *Repo) output(args ...string) (string, error) {
	b, err := xexec.Command(args...).
		WithEnvVars(CleanedGitEnv()).
		WithWorkingDir(r.path).
		WithStdout(nil).
		Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(b)), nil
}

func (r *Repo) BranchExists(ref string) (bool, error) {
	if err := r.run("git", "show-ref", "refs/heads/"+ref); err != nil {
		exitErr := &exec.ExitError{}

		isExitError := errors.As(err, &exitErr)
		if !isExitError {
			return false, err
		}

		// Exit code 1 means the branch doesn't exist
		if exitErr.ExitCode() == 1 {
			return false, nil
		}

		// Unrecognized exit code
		return false, err
	}

	return true, nil
}

// RemoteBranchExists reports whether the remote-tracking ref for the branch
// exists locally, i.e. whether the branch was seen on the remote when it was
// last fetched.
func (r *Repo) RemoteBranchExists(remote string, ref string) (bool, error) {
	if err := r.run("git", "show-ref", fmt.Sprintf("refs/remotes/%s/%s", remote, ref)); err != nil {
		// Exit code 1 means the branch doesn't exist
		if isExitCode(err, 1) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

// DetectMainBranch attempts to automatically detect the main branch name.
// It checks for common branch names ("main", "master") in both local and remote branches,
// returning the first match found.
func (r *Repo) DetectMainBranch() (string, error) {
	candidates := []string{"main", "master"}

	// The trunk branch isn't known yet at this point, so there is no branch to
	// take a remote from: fall back to the repository default. When that can't
	// be determined we only look at local branches.
	remote, remoteErr := r.DefaultRemote()

	for _, candidate := range candidates {
		// Check local branch first
		exists, err := r.BranchExists(candidate)
		if err != nil {
			return "", err
		}

		if exists {
			return candidate, nil
		}

		if remoteErr != nil {
			continue
		}

		// Check remote branch
		exists, err = r.RemoteBranchExists(remote, candidate)
		if err != nil {
			return "", err
		}

		if exists {
			return candidate, nil
		}
	}

	return "", nil
}

func (r *Repo) Checkout(ref string) error {
	return r.run("git", "checkout", ref)
}

func (r *Repo) QuietCheckout(ref string) error {
	return r.run("git", "-c", "core.hooksPath=/dev/null", "checkout", "-q", ref)
}

func (r *Repo) CreateBranch(branch string) error {
	return r.run("git", "checkout", "-b", branch)
}

// CreateTrackingBranch creates a local branch from the same-named branch on the
// given remote, set up to track it. The branch is not checked out. Doing this
// explicitly (rather than relying on git's DWIM when checking out a branch that
// only exists remotely) keeps working when the branch name exists on more than
// one remote, which git refuses to guess between.
func (r *Repo) CreateTrackingBranch(branch string, remote string) error {
	return r.run("git", "branch", "--track", branch, fmt.Sprintf("%s/%s", remote, branch))
}

// CreateBranchFrom creates a new branch based on the given start point (e.g.
// another branch or commit) and switches to it.
func (r *Repo) CreateBranchFrom(branch string, startPoint string) error {
	return r.run("git", "checkout", "-b", branch, startPoint)
}

func (r *Repo) DeleteBranch(branch string) error {
	return xexec.Command("git", "branch", "-D", branch).
		WithEnvVars(CleanedGitEnv()).
		WithWorkingDir(r.path).
		Run()
}

func (r *Repo) GetConfig(key string) (string, error) {
	return r.output("git", "config", key)
}

func (r *Repo) GetCurrentBranchName() (string, error) {
	s, err := r.output("git", "branch", "--show-current")
	if err != nil {
		return "", err
	}

	if s == "" {
		return "", ErrDetachedHead
	}

	return s, nil
}

func (r *Repo) GetLocalBranchNameForCommit(ref string) (string, error) {
	return r.output("git", "branch", "--points-at", ref, "--format=%(refname:lstrip=2)")
}

func (r *Repo) GetForkPoint(branchName string) (ref string, err error) {
	return r.output("git", "merge-base", "--fork-point", branchName)
}

func (r *Repo) GetMergeBase(ref1, ref2 string) (string, error) {
	return r.output("git", "merge-base", ref1, ref2)
}

func (r *Repo) GetCommitHash(ref string) (string, error) {
	return r.output("git", "rev-parse", ref)
}

func (r *Repo) GetShortHash(ref string) (string, error) {
	return r.output("git", "rev-parse", "--short", ref)
}

func (r *Repo) Push() error {
	return xexec.Command("git", "push").
		WithEnvVars(CleanedGitEnv()).
		WithWorkingDir(r.path).
		Run()
}

// RemoteConfig is the git config that decides which remote a branch fetches
// from and pushes to. It is read in one go, so resolving remotes for a whole
// stack of branches doesn't cost a git invocation per branch.
type RemoteConfig struct {
	entries map[string]string
}

// FetchRemoteFor returns the remote branchName is fetched from, which git takes
// from branch.<name>.remote alone — remote.pushDefault and
// branch.<name>.pushRemote only redirect pushes. It returns an empty string
// when that isn't set, which is when git falls back to the repository default
// (see DefaultRemote).
func (c RemoteConfig) FetchRemoteFor(branchName string) string {
	// git lower-cases variable names but leaves the branch name as-is.
	return c.entries["branch."+branchName+".remote"]
}

// PushRemoteFor returns the remote git would push branchName to, following
// git's precedence: branch.<name>.pushRemote, then remote.pushDefault, then the
// remote it fetches from.
func (c RemoteConfig) PushRemoteFor(branchName string) string {
	for _, key := range []string{
		"branch." + branchName + ".pushremote",
		"remote.pushdefault",
	} {
		if remote := c.entries[key]; remote != "" {
			return remote
		}
	}

	return c.FetchRemoteFor(branchName)
}

// RemoteConfig reads the config that decides which remotes branches use.
func (r *Repo) RemoteConfig() (RemoteConfig, error) {
	out, err := r.output("git", "config", "--get-regexp", `^(remote\.pushdefault|branch\..*\.(pushremote|remote))$`)
	if err != nil {
		// Exit code 1 means nothing matched
		if !isExitCode(err, 1) {
			return RemoteConfig{}, err
		}

		out = ""
	}

	entries := map[string]string{}

	for _, line := range strings.Split(out, "\n") {
		if key, value, found := strings.Cut(line, " "); found {
			entries[key] = value
		}
	}

	return RemoteConfig{entries: entries}, nil
}

// DefaultRemote returns the remote git falls back to when nothing is configured
// for the branch: the only remote if the repository has exactly one, otherwise
// "origin" if it exists. This mirrors git's own fallback, so it fails for the
// same ambiguous setups git refuses to guess at.
func (r *Repo) DefaultRemote() (string, error) {
	remotes, err := r.Remotes()
	if err != nil {
		return "", err
	}

	switch {
	case len(remotes) == 0:
		return "", errors.New("repository has no remotes")
	case len(remotes) == 1:
		return remotes[0], nil
	case slices.Contains(remotes, defaultRemoteName):
		return defaultRemoteName, nil
	default:
		return "", fmt.Errorf(
			"cannot determine which remote to use (%s): set remote.pushDefault",
			strings.Join(remotes, ", "),
		)
	}
}

// Remotes returns the names of the configured remotes.
func (r *Repo) Remotes() ([]string, error) {
	out, err := r.output("git", "remote")
	if err != nil {
		return nil, err
	}

	return strings.Fields(out), nil
}

// HasUpstream reports whether the branch has upstream tracking configured.
func (r *Repo) HasUpstream(branchName string) bool {
	merge, _ := r.GetConfig(fmt.Sprintf("branch.%s.merge", branchName))

	return merge != ""
}

// SetUpstream configures the branch to track the same-named branch on the given
// remote. The remote-tracking ref must exist locally, so fetch first if needed.
func (r *Repo) SetUpstream(remote string, branchName string) error {
	configWriteMu.Lock()
	defer configWriteMu.Unlock()

	return r.run("git", "branch", fmt.Sprintf("--set-upstream-to=%s/%s", remote, branchName), branchName)
}

func (r *Repo) ForcePushBranch(remote string, branchName string) error {
	return xexec.Command("git", "push", "--force-with-lease", "-q", remote, branchName).
		WithEnvVars(CleanedGitEnv()).
		WithWorkingDir(r.path).
		WithStdout(nil).
		WithStderr(nil).
		Run()
}

func (r *Repo) FetchBranch(remote string, branchName string) error {
	return r.run("git", "fetch", remote, branchName, "-q")
}

func (r *Repo) GetRemoteCommitHash(remote string, branchName string) (string, error) {
	return r.output("git", "rev-parse", fmt.Sprintf("%s/%s", remote, branchName))
}

func (r *Repo) GetRemoteShortHash(remote string, branchName string) (string, error) {
	return r.output("git", "rev-parse", "--short", fmt.Sprintf("%s/%s", remote, branchName))
}

func (r *Repo) Path() string {
	return r.path
}

func (r *Repo) Rebase(upstream, branchName string) error {
	return xexec.Command("git", "-c", "core.hooksPath=/dev/null", "rebase", upstream, branchName, "--update-refs").
		WithEnvVars(CleanedGitEnv()).
		WithWorkingDir(r.path).
		Run()
}

// RebaseOntoWithBranchPoint rebases branch onto newBase, replaying commits after oldBranchPoint
// This is equivalent to: git rebase --onto <newBase> <oldBranchPoint> <branch>.
func (r *Repo) RebaseOntoWithBranchPoint(newBase, oldBranchPoint, branch string) error {
	return xexec.Command("git", "-c", "core.hooksPath=/dev/null", "rebase", "--onto", newBase, oldBranchPoint, branch, "--update-refs").
		WithEnvVars(CleanedGitEnv()).
		WithWorkingDir(r.path).
		Run()
}

func (r *Repo) Pull() error {
	return xexec.Command("git", "pull", "--ff", "--ff-only").
		WithEnvVars(CleanedGitEnv()).
		WithWorkingDir(r.path).
		Run()
}

func (r *Repo) GitPath() (path string, err error) {
	path, err = r.output("which", "git")
	if err != nil {
		return "", err
	}

	return path, nil
}

func (r *Repo) GitVersion() (*version.Version, error) {
	s, err := r.output("git", "--version")
	if err != nil {
		return nil, err
	}

	v := strings.SplitN(s, " ", 4)
	if len(v) < 3 {
		return nil, fmt.Errorf("unable to parse version from: %s", s)
	}

	versionStr := v[2]

	version, err := version.NewVersion(versionStr)
	if err != nil {
		return nil, err
	}

	return version, nil
}

// HasStagedChanges checks if there are any staged changes in the index.
func (r *Repo) HasStagedChanges() (bool, error) {
	output, err := r.output("git", "diff", "--cached", "--quiet")
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return false, err
		}

		// Exit code 1 means there are differences (staged changes exist)
		if exitErr.ExitCode() == 1 {
			return true, nil
		}

		// Unrecognized exit code
		return false, err
	}

	// Exit code 0 means no differences (no staged changes)
	return output != "", nil
}

// Commit creates an interactive commit, opening an editor for the user to write the commit message.
func (r *Repo) Commit() error {
	return xexec.Command("git", "commit").
		WithEnvVars(CleanedGitEnv()).
		WithWorkingDir(r.path).
		Run()
}

// IsRebaseInProgress checks if a rebase operation is currently in progress.
func (r *Repo) IsRebaseInProgress() (bool, error) {
	// Get the actual git directory (handles both regular repos and linked worktrees)
	gitDir, err := r.output("git", "rev-parse", "--git-dir")
	if err != nil {
		return false, err
	}

	// Check for rebase-merge directory (interactive rebase)
	if err := r.run("test", "-d", gitDir+"/rebase-merge"); err == nil {
		return true, nil
	}

	// Check for rebase-apply directory (non-interactive rebase)
	if err := r.run("test", "-d", gitDir+"/rebase-apply"); err == nil {
		return true, nil
	}

	return false, nil
}

// RebaseContinue continues a rebase operation that was paused due to conflicts.
func (r *Repo) RebaseContinue() error {
	return xexec.Command("git", "-c", "core.hooksPath=/dev/null", "-c", "core.editor=true", "rebase", "--continue").
		WithEnvVars(CleanedGitEnv()).
		WithWorkingDir(r.path).
		Run()
}

// RebaseAbort aborts an in-progress rebase operation.
func (r *Repo) RebaseAbort() error {
	return xexec.Command("git", "rebase", "--abort").
		WithEnvVars(CleanedGitEnv()).
		WithWorkingDir(r.path).
		Run()
}

// HardReset performs a hard reset to the specified commit.
func (r *Repo) HardReset(commit string) error {
	return xexec.Command("git", "reset", "--hard", commit).
		WithEnvVars(CleanedGitEnv()).
		WithWorkingDir(r.path).
		Run()
}
