# gocmdtester

A Go package for testing command-line utilities with coverage support.

## Features

- **Coverage Collection**: Automatically compiles binaries with `-cover` flag and collects coverage data
- **Global Caching**: Compiled binaries are cached by source path, enabling cross-package testing with combined coverage
- **Parallel Test Safety**: Thread-safe cache using sync.Map
- **Clean API**: Simple, intuitive interface with functional options
- **No Dependencies**: Standalone implementation using only standard library and test dependencies

## Requirements

- Go 1.20+ (for binary coverage support with `-cover` flag)
- Go 1.22+ (for `go tool covdata` command to merge coverage)

## Installation

```bash
go get github.com/dansimau/yas/pkg/gocmdtester
```

## Usage

### Basic Example

```go
func TestMyCommand(t *testing.T) {
    // Create a tester from your main.go (compiles once, cached for reuse)
    tester := gocmdtester.FromPath(t, "./cmd/myapp/main.go")
    defer gocmdtester.CleanupAll() // Clean up at end of test

    // Run the binary with arguments
    result := tester.Run("--version")

    // Check the results
    if !result.Success() {
        t.Errorf("command failed: %v", result.Err())
    }

    // Verify output
    if !result.StdoutContains("v1.0.0") {
        t.Errorf("unexpected version output: %s", result.Stdout())
    }
}
```

### With Options

Options are passed to `FromPath()` to configure how the binary runs:

```go
func TestMyCommandWithOptions(t *testing.T) {
    // Configure environment variables, working directory, and stdin at creation time
    input := strings.NewReader("yes\nconfirm\n")
    tester := gocmdtester.FromPath(t, "./cmd/myapp/main.go",
        gocmdtester.WithEnv("DEBUG", "true"),
        gocmdtester.WithWorkingDir("/tmp/deploy"),
        gocmdtester.WithStdin(input))
    defer gocmdtester.CleanupAll()

    // Run uses the configured options
    result := tester.Run("deploy")

    assert.Assert(t, result.Success())
}
```

### Cross-Package Testing with Combined Coverage

The global cache allows multiple test packages to share the same compiled binary and accumulate coverage:

```go
// In pkg/a/a_test.go
func TestFeatureA(t *testing.T) {
    tester := gocmdtester.FromPath(t, "../../cmd/myapp")
    result := tester.Run("feature-a")
    // ... assertions
}

// In pkg/b/b_test.go
func TestFeatureB(t *testing.T) {
    tester := gocmdtester.FromPath(t, "../../cmd/myapp")
    // Same cached binary, same coverage directory
    result := tester.Run("feature-b")
    // ... assertions
}

// In test/main_test.go - collect combined coverage
func TestMain(m *testing.M) {
    code := m.Run()
    gocmdtester.WriteCombinedCoverage("coverage.out")
    gocmdtester.CleanupAll()
    os.Exit(code)
}
```

### Collecting Coverage

```go
func TestMyCommandWithCoverage(t *testing.T) {
    tester := gocmdtester.FromPath(t, "./cmd/myapp/main.go")
    defer gocmdtester.CleanupAll()

    // Run multiple commands to exercise different code paths
    tester.Run("init")
    tester.Run("status")
    tester.Run("deploy", "--dry-run")

    // Write merged coverage profile
    err := tester.WriteCoverageProfile("coverage.out")
    if err != nil {
        t.Errorf("failed to write coverage: %v", err)
    }
}
```

### Advanced: Manual Binary Management

For full control over compilation and cleanup, use the `New` constructor:

```go
func TestManualBinary(t *testing.T) {
    binaryPath := filepath.Join(t.TempDir(), "testbinary")
    coverDir := t.TempDir()

    // Compile once manually
    exec.Command("go", "build", "-cover", "-o", binaryPath, "./cmd/myapp").Run()

    tester := gocmdtester.New(t, binaryPath, coverDir)

    result := tester.Run("feature1")
    assert.Assert(t, result.Success())

    tester.WriteCoverageProfile("coverage.out")
}
```

## API Reference

### Package Functions

#### `FromPath(t *testing.T, mainGoPath string, opts ...Option) *CmdTester`

Creates or returns a cached CmdTester by compiling the binary with coverage enabled. If a tester for this path already exists in the cache, returns a tester that shares the cached binary. Options configure how the binary runs. Calls `t.Fatal` on error.

#### `New(t *testing.T, binaryPath, coverDir string, opts ...Option) *CmdTester`

Creates a CmdTester with a pre-compiled binary. For advanced usage. Testers created with `New` are NOT cached.

#### `CleanupAll() error`

Removes all cached testers and their temporary directories. Call this at the end of testing, typically in TestMain.

#### `WriteCombinedCoverage(path string) error`

Merges coverage from all cached testers into a single file. Useful when multiple test packages share testers and you want combined coverage.

### CmdTester Methods

#### `Run(args ...string) *Result`

Executes the binary with given arguments. Uses the configuration (env, working dir, stdin) set via `FromPath` options. Automatically sets GOCOVERDIR for coverage collection.

#### `WriteCoverageProfile(path string) error`

Merges all collected coverage data for this tester and writes to a file compatible with `go tool cover`.

#### `Cleanup() error`

No-op for cached testers. Use `CleanupAll()` instead to clean up all cached testers.

#### `BinaryPath() string`

Returns the path to the compiled binary.

#### `CoverageDir() string`

Returns the path to the coverage data directory.

### Result

Holds the output and exit code from running a command.

#### `Stdout() string`

Returns stdout output.

#### `Stderr() string`

Returns stderr output.

#### `ExitCode() int`

Returns the exit code.

#### `Err() error`

Returns execution error (nil if binary executed successfully, even with non-zero exit).

#### `Success() bool`

Returns true if command executed successfully with exit code 0.

#### `StdoutContains(s string) bool`

Returns true if stdout contains the substring.

#### `StderrContains(s string) bool`

Returns true if stderr contains the substring.

### Option

Functional options for configuring a CmdTester. Pass these to `FromPath()` or `New()`.

#### `WithEnv(key, value string) Option`

Sets an environment variable for command execution.

#### `WithWorkingDir(path string) Option`

Sets the working directory for command execution.

#### `WithStdin(r io.Reader) Option`

Sets the stdin reader for command execution.

## How It Works

1. **Compilation**: Compiles your binary with `go build -cover` to instrument it for coverage
2. **Execution**: Runs the binary with `GOCOVERDIR` set to collect coverage data
3. **Collection**: Each run appends coverage data to the coverage directory
4. **Merging**: Uses `go tool covdata textfmt` to merge all coverage data into a single profile

## Parallel Testing

The global cache is thread-safe using `sync.Map`, making it safe for parallel test execution. All parallel tests share the same compiled binary and coverage directory:

```go
func TestParallel(t *testing.T) {
    t.Parallel()

    tester := gocmdtester.FromPath(t, "./cmd/myapp/main.go")
    // Note: Don't call CleanupAll() in parallel tests - do it in TestMain

    result := tester.Run("test")
    assert.Assert(t, result.Success())
}
```

## License

Same as the parent repository.
