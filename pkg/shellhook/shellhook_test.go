package shellhook_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/shellhook"
	"gotest.tools/v3/assert"
)

// TestScript_SourcesCommandsAndKeepsExitCode runs the hook in real shells
// around a fake command that asks for a directory change and exits with a
// given code.
func TestScript_SourcesCommandsAndKeepsExitCode(t *testing.T) {
	t.Parallel()

	hook := shellhook.Hook{Command: "fakecmd", EnvVar: "FAKECMD_SHELL_EXEC"}

	for _, shell := range []string{"bash", "zsh"} {
		for _, exitCode := range []string{"0", "3"} {
			t.Run(shell+"/exit"+exitCode, func(t *testing.T) {
				t.Parallel()

				if _, err := exec.LookPath(shell); err != nil {
					t.Skip(shell + " not installed")
				}

				binDir := t.TempDir()

				targetDir, err := filepath.EvalSymlinks(t.TempDir())
				assert.NilError(t, err)

				fake := "#!/bin/sh\necho \"cd '$1'\" >> \"$FAKECMD_SHELL_EXEC\"\nexit $2\n"
				assert.NilError(t, os.WriteFile(filepath.Join(binDir, "fakecmd"), []byte(fake), 0o755))

				script := hook.Script(shell) + "\n" +
					"fakecmd \"$TARGET_DIR\" " + exitCode + "\n" +
					"echo \"exit=$? pwd=$(pwd -P) env=${FAKECMD_SHELL_EXEC:-unset}\"\n"

				cmd := exec.Command(shell, "-c", script)

				cmd.Env = append(os.Environ(),
					"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
					"TARGET_DIR="+targetDir,
				)

				out, err := cmd.CombinedOutput()
				assert.NilError(t, err, string(out))
				assert.Equal(t, strings.TrimSpace(string(out)), "exit="+exitCode+" pwd="+targetDir+" env=unset")
			})
		}
	}
}
