package gocmdtester

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dansimau/yas/pkg/xexec"
)

// cache stores compiled testers keyed by absolute source path.
// This allows multiple test packages to share the same compiled binary
// and accumulate coverage data.
var cache sync.Map // map[string]*CmdTester

// CmdTester manages a compiled Go binary for testing purposes.
// It handles compilation with coverage enabled, running the binary in isolated
// environments, and merging coverage data from multiple runs.
//
// Requirements:
//   - Go 1.20+ for binary coverage support (-cover flag)
//   - Go 1.22+ for go tool covdata command
//
// Example usage:
//
//	func TestMyCommand(t *testing.T) {
//	    tester := gocmdtester.FromPath(t, "./cmd/myapp/main.go",
//	        gocmdtester.WithEnv("DEBUG", "true"))
//	    defer gocmdtester.CleanupAll() // Clean up after test
//
//	    result := tester.Run("--help")
//	    if !result.Success() {
//	        t.Errorf("command failed: %v", result.Err())
//	    }
//	}
type CmdTester struct {
	t              *testing.T
	binaryPath     string
	coverDir       string
	mainGoPath     string
	cleanup        func() error  // Internal cleanup function
	runConfig      *runConfig    // Configuration for Run invocations
	mockRegistry   *mockRegistry // nil until first Mock() call
	skipMockVerify bool          // Skip automatic mock verification
}

// FromPath returns a CmdTester for the Go binary at mainGoPath.
// If a tester for this path already exists in the cache, it returns
// the cached instance. Otherwise, it compiles the binary with coverage
// enabled and caches the result.
//
// The mainGoPath should be a path to a directory containing a main.go file,
// or a path to the main.go file itself.
//
// Options like WithEnv, WithWorkingDir, and WithStdin configure how the
// binary will be run when calling Run().
//
// Because testers are cached, calling Cleanup() on individual testers is a no-op.
// Use ClearCache() or CleanupAll() to clean up all cached testers.
//
// Example:
//
//	tester := gocmdtester.FromPath(t, "./cmd/myapp/main.go",
//	    gocmdtester.WithEnv("DEBUG", "true"),
//	    gocmdtester.WithWorkingDir("/tmp/test"))
//	// No need for defer tester.Cleanup() - testers are cached
func FromPath(t *testing.T, mainGoPath string, opts ...Option) *CmdTester {
	t.Helper()

	// Resolve to absolute path (this becomes the cache key)
	absMainPath, err := filepath.Abs(mainGoPath)
	if err != nil {
		t.Fatalf("failed to resolve main.go path: %v", err)
	}

	// Check cache first
	if cached, ok := cache.Load(absMainPath); ok {
		cachedTester := cached.(*CmdTester)

		// Start with a fresh config - don't inherit options from the cached tester
		// as those were specific to the original caller
		cfg := &runConfig{}
		for _, opt := range opts {
			opt(cfg)
		}

		// Return a new tester that shares the binary but has its own config
		return &CmdTester{
			t:          t,
			binaryPath: cachedTester.binaryPath,
			coverDir:   cachedTester.coverDir,
			mainGoPath: cachedTester.mainGoPath,
			cleanup:    nil, // No cleanup for derived testers
			runConfig:  cfg,
		}
	}

	// Not cached - compile and store
	tester, err := compile(absMainPath, opts...)
	if err != nil {
		t.Fatal(err)
	}

	tester.t = t

	// Store in cache (use LoadOrStore to handle race conditions)
	actual, loaded := cache.LoadOrStore(absMainPath, tester)

	// If another goroutine stored first, clean up our compilation and return theirs
	if loaded {
		_ = tester.cleanup()

		cachedTester := actual.(*CmdTester)

		// Start with a fresh config - don't inherit options from the cached tester
		cfg := &runConfig{}
		for _, opt := range opts {
			opt(cfg)
		}

		return &CmdTester{
			t:          t,
			binaryPath: cachedTester.binaryPath,
			coverDir:   cachedTester.coverDir,
			mainGoPath: cachedTester.mainGoPath,
			cleanup:    nil,
			runConfig:  cfg,
		}
	}

	return tester
}

// compile creates a new CmdTester by compiling the Go binary at absMainPath
// with coverage enabled. This is an internal function called by FromPath.
func compile(absMainPath string, opts ...Option) (*CmdTester, error) {
	// Determine the directory containing main.go
	mainGoDir := absMainPath
	if filepath.Ext(absMainPath) == ".go" {
		mainGoDir = filepath.Dir(absMainPath)
	}

	// Create temp directory for binary
	binaryDir, err := os.MkdirTemp("", "gocmdtester-bin-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory for binary: %w", err)
	}

	// Create temp directory for coverage data
	coverDir, err := os.MkdirTemp("", "gocmdtester-cover-")
	if err != nil {
		_ = os.RemoveAll(binaryDir)

		return nil, fmt.Errorf("failed to create temp directory for coverage: %w", err)
	}

	// Determine binary name from directory
	binaryName := filepath.Base(mainGoDir)
	if binaryName == "." || binaryName == "/" {
		binaryName = "testbinary"
	}

	binaryPath := filepath.Join(binaryDir, binaryName)

	// Get coverpkg from the environment variable GOCOVERPKG
	coverPkg := os.Getenv("GOCMDTESTER_COVERPKG")
	if coverPkg == "" {
		coverPkg = "./..."
	}

	// Compile with coverage enabled
	// Set the working directory to the module directory so go build can find the module
	if err := xexec.Command("go", "build", "-cover", "-coverpkg="+coverPkg, "-covermode=atomic", "-o", binaryPath, ".").
		WithWorkingDir(mainGoDir).
		Verbose(true).
		Run(); err != nil {
		_ = os.RemoveAll(binaryDir)
		_ = os.RemoveAll(coverDir)

		return nil, fmt.Errorf("failed to compile binary with coverage: %w", err)
	}

	// Apply options
	cfg := &runConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Create CmdTester
	tester := &CmdTester{
		binaryPath: binaryPath,
		coverDir:   coverDir,
		mainGoPath: absMainPath,
		runConfig:  cfg,
	}

	// Set cleanup function that removes both temp directories
	tester.cleanup = func() error {
		var errs []error

		if err := os.RemoveAll(binaryDir); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove binary directory: %w", err))
		}

		if err := os.RemoveAll(coverDir); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove coverage directory: %w", err))
		}

		if len(errs) > 0 {
			return errors.Join(errs...)
		}

		return nil
	}

	return tester, nil
}

// New creates a new CmdTester with a pre-compiled binary and coverage directory.
// Testers created with New are NOT cached.
//
// Example:
//
//	func TestMyApp(t *testing.T) {
//	    binaryPath := filepath.Join(t.TempDir(), "testbinary")
//	    coverDir := t.TempDir()
//
//	    // Compile once manually
//	    exec.Command("go", "build", "-cover", "-o", binaryPath, "./cmd/myapp").Run()
//
//	    tester := gocmdtester.New(t, binaryPath, coverDir)
//	    result := tester.Run("--help")
//	}
func New(t *testing.T, binaryPath, coverDir string, opts ...Option) *CmdTester {
	t.Helper()

	cfg := &runConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	return &CmdTester{
		t:          t,
		binaryPath: binaryPath,
		coverDir:   coverDir,
		runConfig:  cfg,
	}
}

// CleanupAll removes all cached testers and their temporary directories.
// Call this at the end of testing, typically in TestMain or after all tests complete.
//
// Example:
//
//	func TestMain(m *testing.M) {
//	    code := m.Run()
//	    gocmdtester.WriteCombinedCoverage("coverage.out")
//	    gocmdtester.CleanupAll()
//	    os.Exit(code)
//	}
func CleanupAll() error {
	var errs []error

	cache.Range(func(key, value any) bool {
		tester := value.(*CmdTester)
		if tester.cleanup != nil {
			if err := tester.cleanup(); err != nil {
				errs = append(errs, err)
			}
		}

		cache.Delete(key)

		return true
	})

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// WriteCombinedCoverage merges coverage from all cached testers into a single file.
// This is useful when multiple test packages share testers and you want combined coverage.
//
// Example:
//
//	func TestMain(m *testing.M) {
//	    code := m.Run()
//	    gocmdtester.WriteCombinedCoverage("coverage.out")
//	    gocmdtester.CleanupAll()
//	    os.Exit(code)
//	}
func WriteCombinedCoverage(path string) error {
	var coverDirs []string

	cache.Range(func(key, value any) bool {
		tester := value.(*CmdTester)
		coverDirs = append(coverDirs, tester.coverDir)

		return true
	})

	if len(coverDirs) == 0 {
		return nil // No coverage to write
	}

	// Join all coverage directories with comma for -i flag
	if err := xexec.Command("go", "tool", "covdata", "textfmt",
		"-i="+strings.Join(coverDirs, ","),
		"-o="+path).
		Verbose(true).
		Run(); err != nil {
		return fmt.Errorf("failed to merge coverage data: %w", err)
	}

	return nil
}

// Run executes the compiled binary with the given arguments.
// It uses the configuration (env, working dir, stdin) set via FromPath options.
// It automatically sets GOCOVERDIR to collect coverage data.
//
// The method uses t.Helper() for proper test line reporting.
//
// Example:
//
//	result := tester.Run("--version")
//	if !result.Success() {
//	    t.Errorf("command failed: %v", result.Err())
//	}
//
//	result := tester.Run("config", "set", "--key", "value")
func (ct *CmdTester) Run(args ...string) *Result {
	ct.t.Helper()

	cfg := ct.runConfig
	if cfg == nil {
		cfg = &runConfig{}
	}

	// Write mock config if mocks are present
	if ct.mockRegistry != nil {
		if err := ct.mockRegistry.writeConfig(); err != nil {
			return &Result{
				err: fmt.Errorf("failed to write mock config: %w", err),
			}
		}
	}

	// Build environment with GOCOVERDIR
	env := os.Environ()

	// Filter out any existing GOCOVERDIR, PATH (we'll reconstruct it), and GOCMDTESTER_MOCK_DIR
	filteredEnv := make([]string, 0, len(env)+len(cfg.env)+3)

	var currentPath string

	for _, e := range env {
		if strings.HasPrefix(e, "GOCOVERDIR=") {
			continue
		}

		if strings.HasPrefix(e, "GOCMDTESTER_MOCK_DIR=") {
			continue
		}

		if strings.HasPrefix(e, "PATH=") {
			currentPath = strings.TrimPrefix(e, "PATH=")

			continue
		}

		filteredEnv = append(filteredEnv, e)
	}

	// Add our GOCOVERDIR
	filteredEnv = append(filteredEnv, "GOCOVERDIR="+ct.coverDir)

	// If mocks are present, set up mock environment
	if ct.mockRegistry != nil {
		filteredEnv = append(filteredEnv, "GOCMDTESTER_MOCK_DIR="+ct.mockRegistry.getMockDir())
		// Store original PATH for passthrough execution
		filteredEnv = append(filteredEnv, "GOCMDTESTER_ORIGINAL_PATH="+currentPath)
		// Prepend shims directory to PATH
		filteredEnv = append(filteredEnv, "PATH="+ct.mockRegistry.getShimsDir()+":"+currentPath)
	} else {
		// No mocks, just use original PATH
		filteredEnv = append(filteredEnv, "PATH="+currentPath)
	}

	// Add custom environment variables
	for k, v := range cfg.env {
		filteredEnv = append(filteredEnv, k+"="+v)
	}

	// Build command with explicit capture buffers
	var stdoutBuf, stderrBuf bytes.Buffer

	cmdArgs := append([]string{ct.binaryPath}, args...)
	cmd := xexec.Command(cmdArgs...).
		WithEnvVars(filteredEnv).
		WithStdout(io.MultiWriter(os.Stdout, &stdoutBuf)).
		WithStderr(io.MultiWriter(os.Stderr, &stderrBuf)).
		Verbose(true)

	if cfg.workingDir != "" {
		cmd.WithWorkingDir(cfg.workingDir)
	}

	if cfg.stdin != nil {
		cmd.WithStdin(cfg.stdin)
	}

	// Run the command
	exitCode := 0

	err := cmd.Run()
	if err != nil {
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			// Binary executed but returned non-zero exit code
			exitCode = exitErr.ExitCode()
		}
	}

	return &Result{
		stdout:   stdoutBuf.String(),
		stderr:   stderrBuf.String(),
		exitCode: exitCode,
		err:      err,
	}
}

// WriteCoverageProfile merges all collected coverage data and writes it
// to the specified file path in text format compatible with go tool cover.
//
// Requires Go 1.22+ for the go tool covdata command.
//
// Example:
//
//	err := tester.WriteCoverageProfile("coverage.out")
//	if err != nil {
//	    t.Errorf("failed to write coverage: %v", err)
//	}
func (ct *CmdTester) WriteCoverageProfile(path string) error {
	if err := xexec.Command("go", "tool", "covdata", "textfmt",
		"-i="+ct.coverDir,
		"-o="+path).
		Verbose(true).
		Run(); err != nil {
		return fmt.Errorf("failed to merge coverage data: %w", err)
	}

	return nil
}

// Cleanup is a no-op for cached testers. Use CleanupAll() or ClearCache() instead.
//
// This method exists for backward compatibility and to prevent accidentally
// cleaning up a shared tester from one test while other tests are still using it.
func (ct *CmdTester) Cleanup() error {
	// No-op for cached testers - use CleanupAll() or ClearCache() instead
	return nil
}

// BinaryPath returns the path to the compiled binary.
// This can be useful for debugging or for running the binary directly.
func (ct *CmdTester) BinaryPath() string {
	return ct.binaryPath
}

// CoverageDir returns the path to the coverage data directory.
// This can be useful for debugging or manual inspection.
func (ct *CmdTester) CoverageDir() string {
	return ct.coverDir
}

// Mock creates a mock for the given command and arguments.
// The mock intercepts calls to the command when Run() is called.
// Returns a Mock that can be configured with builder methods.
//
// All mocks are automatically verified at the end of the test.
// If any mock was not called, the test will fail.
// Use SkipMockVerification() to disable this check for tests that
// intentionally don't call all mocks.
//
// Example:
//
//	mock := tester.Mock("gh", "pr", "create").
//	    WithStdout("https://github.com/user/repo/pull/1").
//	    WithCode(0)
//
//	result := tester.Run("submit")
//
//	if !mock.Called() {
//	    t.Error("expected gh pr create to be called")
//	}
func (ct *CmdTester) Mock(command string, args ...string) *Mock {
	ct.t.Helper()

	// Create mock registry if this is the first mock
	if ct.mockRegistry == nil {
		registry, err := newMockRegistry()
		if err != nil {
			ct.t.Fatalf("failed to create mock registry: %v", err)
		}

		ct.mockRegistry = registry

		// Register automatic verification at test cleanup
		ct.t.Cleanup(func() {
			if ct.skipMockVerify {
				return
			}

			if err := ct.mockRegistry.verifyAllCalled(); err != nil {
				ct.t.Errorf("mock verification failed: %v", err)
			}
		})
	}

	mock := &Mock{
		command:  command,
		args:     args,
		exitCode: 0,
	}

	if err := ct.mockRegistry.addMock(mock); err != nil {
		ct.t.Fatalf("failed to add mock: %v", err)
	}

	return mock
}

// SkipMockVerification disables the automatic verification that all mocks
// were called. Use this for tests that intentionally set up mocks that
// may not be invoked.
func (ct *CmdTester) SkipMockVerification() {
	ct.skipMockVerify = true
}
