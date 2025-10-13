// Package test contains all integration tests for the yas tool.
package test

import (
	"os"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
)

func TestMain(m *testing.M) {
	cleanup := testutil.WithEnv(append(os.Environ(), "XEXEC_VERBOSE=1")...)

	exitCode := m.Run()

	cleanup()

	os.Exit(exitCode)
}
