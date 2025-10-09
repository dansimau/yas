package gitexec

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/dansimau/yas/pkg/xexec"
	"github.com/hashicorp/go-version"
)

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
	if err := r.run("git", "show-ref", fmt.Sprintf("refs/heads/%s", ref)); err != nil {
		exitErr, isExitError := err.(*exec.ExitError)
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

func (r *Repo) RemoteBranchExists(ref string) (bool, error) {
	if err := r.run("git", "show-ref", "refs/remotes/origin/"+ref); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
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

// DetectMainBranch attempts to automatically detect the main branch name.
// It checks for common branch names ("main", "master") in both local and remote branches,
// returning the first match found.
func (r *Repo) DetectMainBranch() (string, error) {
	candidates := []string{"main", "master"}

	for _, candidate := range candidates {
		// Check local branch first
		exists, err := r.BranchExists(candidate)
		if err != nil {
			return "", err
		}
		if exists {
			return candidate, nil
		}

		// Check remote branch
		exists, err = r.RemoteBranchExists(candidate)
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
		return "", errors.New("currently in detached state")
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

func (r *Repo) ForcePushBranch(branchName string) error {
	return xexec.Command("git", "push", "--force-with-lease", "origin", branchName).
		WithEnvVars(CleanedGitEnv()).
		WithWorkingDir(r.path).
		Run()
}

func (r *Repo) FetchBranch(branchName string) error {
	return r.run("git", "fetch", "origin", branchName)
}

func (r *Repo) GetRemoteCommitHash(branchName string) (string, error) {
	return r.output("git", "rev-parse", fmt.Sprintf("origin/%s", branchName))
}

func (r *Repo) GetRemoteShortHash(branchName string) (string, error) {
	return r.output("git", "rev-parse", "--short", fmt.Sprintf("origin/%s", branchName))
}

func (r *Repo) Rebase(upstream, branchName string) error {
	return xexec.Command("git", "-c", "core.hooksPath=/dev/null", "rebase", upstream, branchName, "--update-refs").
		WithEnvVars(CleanedGitEnv()).
		WithWorkingDir(r.path).
		Run()
}

// RebaseOntoWithBranchPoint rebases branch onto newBase, replaying commits after oldBranchPoint
// This is equivalent to: git rebase --onto <newBase> <oldBranchPoint> <branch>
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
