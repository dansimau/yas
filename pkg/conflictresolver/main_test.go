package conflictresolver_test

import (
	"os"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
)

func TestMain(m *testing.M) {
	restoreHome := testutil.IsolateHome()

	exitCode := m.Run()

	restoreHome()

	os.Exit(exitCode)
}
