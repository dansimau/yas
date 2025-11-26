# Test Conversion Guide

This guide describes how to convert tests from the old pattern (using `testutil.WithTempWorkingDir` and `yascli.Run`) to the new pattern (using `gocmdtester` and explicit working directories).

## Important: Use CLI Instead of Internal APIs

**Always use `cli.Run()` (via `gocmdtester`) instead of calling internal package functions directly.**

**Why this matters:**

1. **Mocks only work through CLI** - When you call internal functions like `yas.NewFromRepository()` or `y.Submit()`, command mocks set up via `cli.Mock()` won't be used. The code will try to execute real commands.

2. **Environment isolation** - `gocmdtester` runs the CLI in a subprocess with a controlled environment. Direct function calls run in the test process, which can modify the test's working directory or environment variables.

3. **Realistic testing** - Using the CLI tests the actual user-facing behavior, including argument parsing and error handling.

**Before (problematic):**

```go
// BAD: Direct internal API calls bypass mocks and can affect test environment
y, err := yas.NewFromRepository(tempDir)
assert.NilError(t, err)
err = y.Submit(false)  // This won't use your gh mocks!
```

**After (correct):**

```go
// GOOD: CLI calls use mocks and run in isolated subprocess
assert.NilError(t, cli.Run("submit").Err())
```

**Exception:** Reading state for assertions is acceptable since it doesn't execute commands:

```go
// OK: Reading state for verification (no commands executed)
state, err := yas.LoadRestackState(tempDir)
assert.NilError(t, err)
assert.Equal(t, state.CurrentBranch, "topic-a")
```

## Key Changes Overview

| Old Pattern                                      | New Pattern                                            |
| ------------------------------------------------ | ------------------------------------------------------ |
| `testutil.WithTempWorkingDir(t, func() { ... })` | `tempDir := t.TempDir()`                               |
| `yascli.Run("cmd", "args")`                      | `cli.Run("cmd", "args").Err()` or `.ExitCode()`        |
| `testutil.ExecOrFail(t, "...")`                  | `testutil.ExecOrFailWithWorkingDir(t, tempDir, "...")` |
| `mustExecOutput("git", "...")`                   | `mustExecOutputWithWorkingDir(tempDir, "git", "...")`  |
| Relative paths like `"."`                        | Explicit `tempDir` variable                            |
| No parallel execution                            | `t.Parallel()` at start of each test                   |

## Step-by-Step Conversion

### 1. Add Parallel Execution

Add `t.Parallel()` as the first line of each test function:

```go
func TestSomething(t *testing.T) {
    t.Parallel()
    // ...
}
```

### 2. Replace Working Directory Setup

**Before:**

```go
testutil.WithTempWorkingDir(t, func() {
    // test code
})
```

**After:**

```go
tempDir := t.TempDir()

cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
    gocmdtester.WithWorkingDir(tempDir),
)
```

### 3. Update Imports

**Remove:**

```go
"github.com/dansimau/yas/pkg/yascli"
```

**Add:**

```go
"github.com/dansimau/yas/pkg/gocmdtester"
```

Also keep or add as needed:

```go
"github.com/dansimau/yas/pkg/stringutil"  // for MustInterpolate
"github.com/dansimau/yas/pkg/testutil"    // for ExecOrFailWithWorkingDir
```

### 4. Replace CLI Invocations

**Before:**

```go
assert.Equal(t, yascli.Run("add", "branch", "--parent=main"), 0)
exitCode := yascli.Run("restack")
assert.Equal(t, exitCode, 1)
```

**After:**

```go
assert.NilError(t, cli.Run("add", "branch", "--parent=main").Err())
result := cli.Run("restack")
assert.Equal(t, result.ExitCode(), 1)
```

(Can also use `assert.NilError(t, result.Err())` if checking for error code 0)

### 5. Replace Shell Command Execution

**Before:**

```go
testutil.ExecOrFail(t, `
    git init --initial-branch=main
    git checkout -b topic-a
`)
```

**After:**

```go
testutil.ExecOrFailWithWorkingDir(t, tempDir, `
    git init --initial-branch=main
    git checkout -b topic-a
`)
```

### 6. Replace Output Capture

**Before:**

```go
output := mustExecOutput("git", "rev-parse", "HEAD")
```

**After:**

```go
output := mustExecOutputWithWorkingDir(tempDir, "git", "rev-parse", "HEAD")
```

### 7. Replace Relative Paths

Any use of `"."` as a directory should be replaced with `tempDir`:

**Before:**

```go
y, err := yas.NewFromRepository(".")
state, err := yas.LoadRestackState(".")
assertRestackStateExists(t, ".")
```

**After:**

```go
y, err := yas.NewFromRepository(tempDir)
state, err := yas.LoadRestackState(tempDir)
assertRestackStateExists(t, tempDir)
```

### 8. Update Config Writing

**Before:**

```go
cfg := yas.Config{
    RepoDirectory: ".",
    TrunkBranch:   "main",
}
_, err := yas.WriteConfig(cfg)
```

**After:**

```go
_, err := yas.WriteConfig(yas.Config{
    RepoDirectory: tempDir,
    TrunkBranch:   "main",
})
```

## Mocking External Commands

The `gocmdtester` package provides command mocking capabilities for tests that interact with external tools like `gh` (GitHub CLI).

### Basic Mocking

```go
cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
    gocmdtester.WithWorkingDir(tempDir),
)

// Mock a command with specific arguments
cli.Mock("gh", "pr", "view", "42", "--json", "body", "-q", ".body").
    WithStdout("PR body content")

// Mock a command that should succeed silently
cli.Mock("gh", "pr", "edit", "42", "--body", "New body")
```

### Using the mockGitHubPRForBranch Helper

For PR-related tests, use the existing helper:

```go
mockGitHubPRForBranch(cli, "branch-name", yas.PullRequestMetadata{
    URL:         "https://github.com/test/test/pull/42",
    BaseRefName: "main",
})
```

### Setting Up a Fake Remote

When tests need to push/fetch, create a bare repository as a fake origin:

```go
fakeOrigin := t.TempDir()

testutil.ExecOrFailWithWorkingDir(t, tempDir, stringutil.MustInterpolate(`
    # Set up "remote" repository
    git init --bare {{.fakeOrigin}}

    git init --initial-branch=main
    git remote add origin {{.fakeOrigin}}

    # ... rest of setup

    git push origin branch-name
`, map[string]string{
    "fakeOrigin": fakeOrigin,
}))
```

## Complete Example

**Before:**

```go
func TestExample(t *testing.T) {
    testutil.WithTempWorkingDir(t, func() {
        testutil.ExecOrFail(t, `
            git init --initial-branch=main
            touch main
            git add main
            git commit -m "main-0"
        `)

        cfg := yas.Config{
            RepoDirectory: ".",
            TrunkBranch:   "main",
        }
        _, err := yas.WriteConfig(cfg)
        assert.NilError(t, err)

        assert.Equal(t, yascli.Run("add", "topic-a", "--parent=main"), 0)

        output := mustExecOutput("git", "branch", "--show-current")
        equalLines(t, output, "main")
    })
}
```

**After:**

```go
func TestExample(t *testing.T) {
    t.Parallel()

    tempDir := t.TempDir()

    cli := gocmdtester.FromPath(t, "../cmd/yas/main.go",
        gocmdtester.WithWorkingDir(tempDir),
    )

    testutil.ExecOrFailWithWorkingDir(t, tempDir, `
        git init --initial-branch=main
        touch main
        git add main
        git commit -m "main-0"
    `)

    _, err := yas.WriteConfig(yas.Config{
        RepoDirectory: tempDir,
        TrunkBranch:   "main",
    })
    assert.NilError(t, err)

    assert.NilError(t, cli.Run("add", "topic-a", "--parent=main").Err())

    output := mustExecOutputWithWorkingDir(tempDir, "git", "branch", "--show-current")
    equalLines(t, output, "main")
}
```

## Checklist

When converting a test, verify:

- [ ] `t.Parallel()` added at start of test
- [ ] `tempDir := t.TempDir()` replaces `WithTempWorkingDir`
- [ ] `gocmdtester.FromPath` creates CLI instance with working dir
- [ ] All `yascli.Run()` calls converted to `cli.Run().Err()` or `.ExitCode()`
- [ ] **All direct internal API calls (e.g., `y.Submit()`, `y.SetParent()`) replaced with `cli.Run()`**
- [ ] All `testutil.ExecOrFail` converted to `ExecOrFailWithWorkingDir`
- [ ] All `mustExecOutput` converted to `mustExecOutputWithWorkingDir`
- [ ] All relative paths (`"."`) replaced with `tempDir`
- [ ] All `yas.Config{RepoDirectory: "."}` updated to use `tempDir`
- [ ] Imports updated (remove `yascli`, add `gocmdtester`)
- [ ] External command mocks added if test uses `gh` CLI
- [ ] Fake origin created if test needs to push/fetch
- [ ] Test runs successfully with `go test -v`
- [ ] Test runs in parallel without conflicts
