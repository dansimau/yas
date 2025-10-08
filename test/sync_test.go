package test

import (
	"os"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/testutil"
	"github.com/dansimau/yas/pkg/yas"
	"github.com/dansimau/yas/pkg/yascli"
	"gotest.tools/v3/assert"
)

func TestSyncRestacksChildrenOfMergedParent(t *testing.T) {
	_, cleanup := setupMockCommandsWithPR(t, mockPROptions{})
	defer cleanup()

	setEnv := func(key, value string) {
		original := os.Getenv(key)
		assert.NilError(t, os.Setenv(key, value))
		t.Cleanup(func() {
			if original == "" {
				os.Unsetenv(key)
			} else {
				os.Setenv(key, original)
			}
		})
	}

	setEnv("YAS_TEST_EXISTING_PR_ID_PARENT", "PR_parent")
	setEnv("YAS_TEST_PR_STATE_PARENT", "MERGED")
	setEnv("YAS_TEST_EXISTING_PR_ID_CHILD", "PR_child")
	setEnv("YAS_TEST_PR_STATE_CHILD", "OPEN")

	testutil.WithTempWorkingDir(t, func() {
		testutil.ExecOrFail(t, `
                        git init --initial-branch=main
                        touch main
                        git add main
                        git commit -m "main-0"

                        git checkout -b parent
                        touch parent
                        git add parent
                        git commit -m "parent-0"

                        git checkout -b child
                        touch child
                        git add child
                        git commit -m "child-0"

                        git checkout main
                        git merge --ff-only parent

                        git checkout child
                `)

		assert.Equal(t, yascli.Run("config", "set", "--trunk-branch=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=parent", "--parent=main"), 0)
		assert.Equal(t, yascli.Run("add", "--branch=child", "--parent=parent"), 0)

		y, err := yas.NewFromRepository(".")
		assert.NilError(t, err)
		assert.NilError(t, y.RefreshRemoteStatus("parent", "child"))

		assert.Equal(t, yascli.Run("sync"), 0)

		equalLines(t, mustExecOutput("git", "branch", "--list", "parent"), "")

		y, err = yas.NewFromRepository(".")
		assert.NilError(t, err)

		childMetadata, exists := y.TrackedBranches().Get("child")
		assert.Assert(t, exists)
		assert.Equal(t, childMetadata.Parent, "main")

		mainHead := strings.TrimSpace(mustExecOutput("git", "rev-parse", "main"))
		mergeBase := strings.TrimSpace(mustExecOutput("git", "merge-base", "child", "main"))
		assert.Equal(t, mergeBase, mainHead)
	})
}
