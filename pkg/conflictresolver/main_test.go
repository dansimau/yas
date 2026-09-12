package conflictresolver_test

import (
	"os"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
)

func TestMain(m *testing.M) {
	restoreGitConfig := testutil.IsolateGitConfig()

	exitCode := m.Run()

	restoreGitConfig()

	os.Exit(exitCode)
}
