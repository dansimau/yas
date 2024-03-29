package gitexec

import (
	"os"
	"strings"
)

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
