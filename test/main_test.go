// Package test contains all integration tests for the yas tool.
package test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/xexec"
)

func TestMain(m *testing.M) {
	cleanup := testutil.WithEnv(append(os.Environ(), "XEXEC_VERBOSE=1")...)

	// Strip YAS_SHELL_EXEC from env so it doesn't interfere with tests
	if err := os.Unsetenv("YAS_SHELL_EXEC"); err != nil {
		panic(err)
	}

	pinGoEnv()

	restoreHome := testutil.IsolateHome()

	exitCode := m.Run()

	err := gocmdtester.WriteCombinedCoverage("../coverage/integration-tests.cov")
	if err != nil {
		panic(err)
	}

	restoreHome()
	cleanup()

	_ = gocmdtester.CleanupAll()

	os.Exit(exitCode)
}

// pinGoEnv sets the Go environment variables whose defaults derive from HOME
// to their current effective values. gocmdtester compiles yas with `go build`
// from inside this process, so without this, pointing HOME at an empty
// directory would make Go lose its build and module caches and recompile
// (and re-download) everything on every run.
func pinGoEnv() {
	out, err := xexec.Command("go", "env", "-json", "GOENV", "GOPATH", "GOCACHE", "GOMODCACHE").Output()
	if err != nil {
		panic(err)
	}

	values := map[string]string{}
	if err := json.Unmarshal(out, &values); err != nil {
		panic(err)
	}

	for name, value := range values {
		if err := os.Setenv(name, value); err != nil {
			panic(err)
		}
	}
}
