package gitexec

import (
	"os"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
)

func TestMain(m *testing.M) {
	cleanup := testutil.WithEnv(append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+testutil.MustGetFixtureFilePath("../../test/gitconfig"),
	)...)

	exitCode := m.Run()

	cleanup()

	os.Exit(exitCode)
}
