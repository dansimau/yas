// Package test contains all integration tests for the yas tool.
package test

import (
	"os"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
)

func TestMain(m *testing.M) {
	cleanup := testutil.WithEnv(append(os.Environ(), "XEXEC_VERBOSE=1")...)
	restoreGitConfig := testutil.IsolateGitConfig()

	// Strip YAS_SHELL_EXEC from env so it doesn't interfere with tests
	if err := os.Unsetenv("YAS_SHELL_EXEC"); err != nil {
		panic(err)
	}

	exitCode := m.Run()

	err := gocmdtester.WriteCombinedCoverage("../coverage/integration-tests.cov")
	if err != nil {
		panic(err)
	}

	restoreGitConfig()
	cleanup()

	_ = gocmdtester.CleanupAll()

	os.Exit(exitCode)
}
