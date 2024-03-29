package gitexec

import (
	"os"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestCleanGitEnvVars(t *testing.T) {
	testEnvName := "GIT_TEST_VAR"
	testEnvValue := "foo"

	_ = os.Setenv(testEnvName, testEnvValue)

	envVars := os.Environ()
	var containsGitVar bool

	for _, envVar := range envVars {
		if strings.HasPrefix(envVar, "GIT_") {
			containsGitVar = true
		}
	}

	assert.Assert(t, containsGitVar)

	cleanedEnvVars := CleanedGitEnv()
	containsGitVar = false

	for _, envVar := range cleanedEnvVars {
		if strings.HasPrefix(envVar, "GIT_") {
			containsGitVar = true
		}
	}

	assert.Assert(t, !containsGitVar)
}
