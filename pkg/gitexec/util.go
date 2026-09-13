package gitexec

import (
	"os"
	"slices"
	"strings"
)

var allowedGitEnvVars = []string{
	"GIT_CONFIG_GLOBAL",
	"GIT_CONFIG_SYSTEM",
}

// CleanedGitEnv ensures we have a clean environment to execute the git
// binary in. If we don't clean this, GIT_ variables from a parent git context
// could interfere with our subcommands (for example, if we are running inside
// a pre-commit hook or on CI).
func CleanedGitEnv() []string {
	newEnv := []string{}

	for _, envVar := range os.Environ() {
		if strings.HasPrefix(envVar, "GIT_") {
			envVarKey, _, _ := strings.Cut(envVar, "=")
			if !slices.Contains(allowedGitEnvVars, envVarKey) {
				continue
			}
		}

		newEnv = append(newEnv, envVar)
	}

	return newEnv
}
