package gitexec

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/dansimau/yas/pkg/xexec"
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

func (r *Repo) output(args ...string) ([]byte, error) {
	return xexec.Command(args...).
		WithEnvVars(CleanedGitEnv()).
		WithWorkingDir(r.path).
		WithStdout(nil).
		Output()
}

func (r *Repo) BranchExists(ref string) (bool, error) {
	err := r.run("git", "show-ref", fmt.Sprintf("refs/heads/%s", ref))
	exitErr, isExitError := err.(*exec.ExitError)
	if !isExitError {
		return false, err
	}

	if exitErr.ExitCode() == 0 {
		return true, nil
	}

	if exitErr.ExitCode() == 1 {
		return false, nil
	}

	// Unrecognized exit code
	return false, err
}

func (r *Repo) Checkout(ref string) error {
	return r.run("git", "checkout", "-q", ref)
}

func (r *Repo) DeleteBranch(branch string) error {
	return r.run("git", "branch", "-D", branch)
}

func (r *Repo) GetShortHash(ref string) (string, error) {
	output, err := r.output("git", "rev-parse", "--short", ref)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

func (r *Repo) Pull() error {
	return xexec.Command("git", "pull", "--ff", "--ff-only").
		WithEnvVars(CleanedGitEnv()).
		WithWorkingDir(r.path).
		Run()
}

// CleanedGitEnv ensures we have a clean environment to execute the git
// binary in. If we don't clean this, GIT_ variables from a parent git context
// could interfere with our subcommands (for example, if we are running inside
// a pre-commit hook or on CI).
func CleanedGitEnv() []string {
	newEnv := []string{}

	for _, envVar := range os.Environ() {
		if strings.HasPrefix(envVar, "GIT_") {
			continue
		}

		newEnv = append(newEnv, envVar)
	}

	return newEnv
}
