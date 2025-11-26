package gocmdtester_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/gocmdtester"
	"gotest.tools/v3/assert"
)

// TestIntegration_YasBinary demonstrates using gocmdtester with the actual yas binary.
// This test shows how to collect coverage from a real CLI application.
func TestIntegration_YasBinary(t *testing.T) {
	t.Parallel()

	// Path to the yas main.go
	mainGoPath := filepath.Join("..", "..", "cmd", "yas", "main.go")

	// Create tester (uses cached binary if already compiled)
	tester := gocmdtester.FromPath(t, mainGoPath)

	// Run the binary with --help flag
	result := tester.Run("--help")

	// Log the output for debugging
	t.Logf("Stdout: %s", result.Stdout())
	t.Logf("Stderr: %s", result.Stderr())
	t.Logf("Exit code: %d", result.ExitCode())

	// Should succeed or exit with code 0 (help is success)
	// Note: Some CLIs exit with 0 for --help, some with 1
	assert.Assert(t, result.ExitCode() == 0 || result.ExitCode() == 1)

	// Should show help information (might be in stdout or stderr)
	hasHelp := result.StdoutContains("yas") || result.StderrContains("yas")
	assert.Assert(t, hasHelp, "Expected help output to contain 'yas'")

	// Write coverage profile to demonstrate coverage collection
	coverFile := filepath.Join(t.TempDir(), "yas-coverage.out")
	err := tester.WriteCoverageProfile(coverFile)
	assert.NilError(t, err)

	// Verify coverage file exists and has content
	content, err := os.ReadFile(coverFile)
	assert.NilError(t, err)
	assert.Assert(t, len(content) > 0)
	assert.Assert(t, strings.Contains(string(content), "mode:"))

	t.Logf("Coverage profile written to: %s", coverFile)
	t.Logf("Coverage file size: %d bytes", len(content))
}
