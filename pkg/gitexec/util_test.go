package gitexec_test

import (
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gitexec"
	"gotest.tools/v3/assert"
)

func TestCleanedGitEnv(t *testing.T) {
	t.Setenv("GIT_TEST_VAR", "foo")
	t.Setenv("GIT_CONFIG_GLOBAL", "/path/to/global")
	t.Setenv("GIT_CONFIG_SYSTEM", "/path/to/system")

	cleaned := envMap(gitexec.CleanedGitEnv())

	_, hasTestVar := cleaned["GIT_TEST_VAR"]
	assert.Assert(t, !hasTestVar, "GIT_TEST_VAR should be removed")

	assert.Equal(t, cleaned["GIT_CONFIG_GLOBAL"], "/path/to/global")
	assert.Equal(t, cleaned["GIT_CONFIG_SYSTEM"], "/path/to/system")
}

// envMap converts a list of KEY=VALUE strings into a map.
func envMap(vars []string) map[string]string {
	m := make(map[string]string, len(vars))

	for _, v := range vars {
		key, value, _ := strings.Cut(v, "=")
		m[key] = value
	}

	return m
}
