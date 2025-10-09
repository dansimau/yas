package testutil

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestParseEnvVar(t *testing.T) {
	t.Parallel()

	envVar := "TEST_ENV_VAR=foo"
	slicedEnvVar := strings.Split(envVar, "=")

	parsedEnvName, parsedEnvValue := parseEnvVar(envVar)

	assert.Assert(t, cmp.Equal(parsedEnvName, slicedEnvVar[0]))
	assert.Assert(t, cmp.Equal(parsedEnvValue, slicedEnvVar[1]))
}
