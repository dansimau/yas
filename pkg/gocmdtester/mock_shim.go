package gocmdtester

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/dansimau/yas/pkg/xexec"
)

var (
	shimBinaryPath string
	shimMu         sync.Mutex
	shimCompiled   bool
)

// ensureShimCompiled compiles the mockshim binary once per process.
// Returns the path to the compiled binary.
func ensureShimCompiled() (string, error) {
	shimMu.Lock()
	defer shimMu.Unlock()

	if shimCompiled {
		return shimBinaryPath, nil
	}

	// Find the mockshim source directory relative to this package
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("failed to get current file path")
	}

	packageDir := filepath.Dir(thisFile)
	mockshimDir := filepath.Join(packageDir, "mockshim")

	// Verify the source exists
	mainGo := filepath.Join(mockshimDir, "main.go")
	if _, err := os.Stat(mainGo); err != nil {
		return "", fmt.Errorf("mockshim source not found at %s: %w", mainGo, err)
	}

	// Create temp directory for the compiled binary
	tmpDir, err := os.MkdirTemp("", "gocmdtester-mockshim-")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	binaryPath := filepath.Join(tmpDir, "mockshim")

	// Compile the mockshim binary
	if err := xexec.Command("go", "build", "-o", binaryPath, ".").
		WithWorkingDir(mockshimDir).
		Verbose(true).
		Run(); err != nil {
		_ = os.RemoveAll(tmpDir)

		return "", fmt.Errorf("failed to compile mockshim: %w", err)
	}

	shimBinaryPath = binaryPath
	shimCompiled = true

	return shimBinaryPath, nil
}
