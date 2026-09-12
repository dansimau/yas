package yas

import "github.com/dansimau/yas/pkg/xexec"

// gh builds a `gh` command that runs inside the selected repository. gh
// derives the GitHub repository from the working directory, so running it in
// the caller's cwd would target the wrong repository (or none at all) when yas
// is pointed at a repository via --repo.
func (yas *YAS) gh(args ...string) *xexec.Cmd {
	return xexec.Command(append([]string{"gh"}, args...)...).
		WithWorkingDir(yas.cfg.RepoDirectory)
}
