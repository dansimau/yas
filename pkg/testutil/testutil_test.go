package testutil_test

import (
	"os"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"gotest.tools/v3/assert"
)

func TestWithEnv(t *testing.T) {
	// Set test environment value
	testEnvName := "TEST_ENV_VAR"
	t.Setenv(testEnvName, "foo")

	_, exists := os.LookupEnv(testEnvName)
	// Check if environment variable exists
	assert.Assert(t, exists)

	// Setup new clean environment
	restoreEnv := testutil.WithEnv()

	// In the clean environment the test environment variable is not expected
	_, exists = os.LookupEnv(testEnvName)
	assert.Assert(t, !exists)

	// Restore environment to original values
	restoreEnv()

	// Check if original var (testEnvVar) is set again
	_, exists = os.LookupEnv(testEnvName)
	assert.Assert(t, exists)
}
