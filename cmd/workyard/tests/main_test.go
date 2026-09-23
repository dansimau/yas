// Package tests contains the black-box integration tests for the workyard
// CLI. Every test drives the compiled binary through gocmdtester.
package tests

import (
	"os"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
)

func TestMain(m *testing.M) {
	cleanup := testutil.WithEnv(append(os.Environ(),
		"XEXEC_VERBOSE=1",
		"GIT_CONFIG_GLOBAL="+testutil.MustGetFixtureFilePath("../../../test/gitconfig"),
	)...)

	// A WORKYARD_ROOT from the developer's shell would break root discovery.
	if err := os.Unsetenv("WORKYARD_ROOT"); err != nil {
		panic(err)
	}

	// Nor should a WORKYARD_SHELL_EXEC from the shell hook receive commands.
	if err := os.Unsetenv("WORKYARD_SHELL_EXEC"); err != nil {
		panic(err)
	}

	exitCode := m.Run()

	if err := os.MkdirAll("../../../coverage", 0o755); err != nil {
		panic(err)
	}

	if err := gocmdtester.WriteCombinedCoverage("../../../coverage/workyard-tests.cov"); err != nil {
		panic(err)
	}

	cleanup()

	_ = gocmdtester.CleanupAll()

	os.Exit(exitCode)
}
