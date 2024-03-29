package test

import (
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yascli"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestMissingGitRepo(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		_, stderr, err := testutil.CaptureOutput(func() {
			exitCode := yascli.Run("list")
			assert.Equal(t, exitCode, 1)
		})

		assert.NilError(t, err)
		assert.Assert(t, cmp.Contains(stderr, "hint: specify --repo"))
	})
}

func TestNotInitialized(t *testing.T) {
	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `git init`)

		_, stderr, err := testutil.CaptureOutput(func() {
			exitCode := yascli.Run("list")
			assert.Equal(t, exitCode, 1)
		})

		assert.NilError(t, err)
		assert.Assert(t, cmp.Contains(stderr, "repository not configured"))
	})
}
