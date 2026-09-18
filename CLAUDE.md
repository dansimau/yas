# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

yas (Yet Another Stacked Diff Tool) is a CLI tool for managing stacked PRs on GitHub, written in Go. It enables developers to create and maintain hierarchical branches where each branch depends on its parent, forming a stack of changes.

## Build, Test & Lint Commands

```bash
# Build the binary
make yas

# Run all tests with coverage
make test

# Run tests in a specific package
go test ./pkg/yas
go test ./test
go test ./cmd/workyard/tests

# Run a specific test
go test ./test -run TestUpdateTrunk

# Run lint checks (formats and fixes)
make lint
```

## Architecture

### Core Package Structure

- **cmd/yas/main.go**: Entry point that delegates to `pkg/yascli`
- **pkg/yascli**: CLI command handlers and argument parsing using go-flags
- **pkg/yas**: Core business logic for stacked diff management
- **cmd/workyard/main.go**: Entry point of the separate `workyard` binary, delegating to `pkg/workyardcli`
- **pkg/workyardcli**: go-flags CLI for workyard (`create`, `status`/`st`, `diff`, `git`, `list`/`ls`, `remove`)
- **pkg/workyard**: Library for creating and operating on workyards (scan, copy/clone, worktree creation, fan-out, removal); no printing, results via return values and callbacks
- **cmd/workyard/tests**: Black-box integration tests for the workyard CLI (same `gocmdtester` pattern as `test/`)
- **pkg/conflictresolver**: Pluggable tools for automatically resolving rebase conflicts (registry + `claude` implementation)
- **pkg/gitexec**: Git operations wrapper using go-git and command execution
- **pkg/xexec**: Command execution utilities with environment control
- **pkg/gocmdtester**: Test utility for running CLI with coverage collection
- **test/**: Integration tests using temporary git repositories

### State Management

yas maintains two key files in the `.yas` directory:

- **.yas/yas.yaml**: Configuration (trunk branch name, conflict resolver settings, etc.)
- **.yas/yas.state.json**: JSON database tracking branch metadata and parent relationships

The state is managed through `yasDatabase` (pkg/yas/store.go) which uses a thread-safe `branchMap` to store `BranchMetadata` for each tracked branch.

### Branch Graph Model

yas uses a Directed Acyclic Graph (DAG) from `github.com/heimdalr/dag` to model branch dependencies:

- Each branch is a vertex containing `BranchMetadata`
- Edges represent parent-child relationships
- The trunk branch (e.g., `main`) is the root vertex
- Graph operations enable walking descendant branches for restack operations

### Key Workflows

**Add/Track Branch** (`yas add`):

- Detects fork point using `git merge-base --fork-point`
- Automatically determines parent branch from fork point
- Stores parent relationship in `.yasstate`

**Submit** (`yas submit`):

- Pushes current branch to remote
- Creates GitHub PR using `gh` CLI with `--draft --fill-first`
- Sets PR base to parent branch (enables stacked PRs)

**Restack** (`yas restack`):

- Builds DAG of all branch relationships
- Gets descendants of current branch
- Rebases each descendant from leaf nodes using `git rebase --update-refs`
- Skips git hooks during rebase with `core.hooksPath=/dev/null`
- On conflict, saves `.yas/yas.restack.json` and (if `conflict-resolver` is not `none`) hands the conflicted files to the resolver (`pkg/yas/conflicts.go`); `after-resolve` decides whether to stop for review, continue when no markers remain, or force
- Settings come from flags (`--conflict-resolver`, `--after-resolve` on restack/sync/move/continue), then the saved restack state (for `continue`), then config; defaults are `none`/`stop`

**Sync** (`yas sync`):

- Tracks untracked branches by refreshing remote status
- Queries GitHub PR status using `gh pr list --json`
- Deletes local branches for merged PRs
- Updates trunk branch with `git pull --ff --ff-only`

**Workyard** (`workyard create`, separate binary in `cmd/workyard`):

- A workyard is a copy of a directory tree (e.g. a multi-repo workspace) in which every git repository becomes a `git worktree` of the source repository, checked out at one branch. Like a git worktree, the yard holds only a pointer back to its source: a `.workyard` *file* at its root (`workyard: <source>`, `id: <id>`). The source keeps per-yard metadata in `.workyard/yards/<id>.json` next to its optional `.workyard/config.yaml`; nothing under `.workyard` is ever copied
- `pkg/workyard.PlanCreate` guards (target empty, no containment except a target under the source's `.workyard/` that does not swallow `.workyard/yards`; the source must not be or be inside a git repository, nor inside a workyard), scans the source depth-first with at most `NumCPU` goroutines (never following symlinks, never descending into repos; a dir is a repo if it has a `.git` entry or a bare layout) and resolves the branch per repo: existing local branch (error if checked out) → remote-only branch (tracking) → tag/commit (detached) → create from trunk (`config.yaml` `trunk`/`repos.<path>.trunk`, else `Repo.DetectMainBranch`). Repos sharing a git common dir get the branch once; the rest are detached
- `Create` writes the metadata (`complete: false`) and pointer first, copies non-repo subtrees (APFS `clonefile` on macOS, plain copy otherwise), adds worktrees with hooks disabled (serialized per common dir), restores ancestor directory modes, then marks the metadata complete. Every step registers an undo in a `rollback` list (worktree + created branch, metadata, target); on failure they run in reverse and any undo failures are reported together
- `status`/`diff`/`git` fan out `git -C <repo>` over every repo in the metadata (`Yard.Run`); unknown options and everything after the first positional are passed to git verbatim; a header is printed only for repos that produced output. Root discovery: `WORKYARD_ROOT`, else walk up to a `.workyard` file
- `remove` removes each worktree through its source repo (refusing dirty ones without `-f`), deletes the directory, prunes, deletes the branches workyard created (only fully merged ones unless forced) and the metadata. A yard whose source is gone is deleted outright (`RemoveOrphan`)

### Environment Management

Git operations use cleaned environments (`CleanedGitEnv()` in pkg/gitexec/util.go) to avoid inheriting unwanted git configuration from the parent process.

### Testing Patterns

Integration tests (in `test/` for yas and `cmd/workyard/tests/` for workyard) use `gocmdtester.FromPath()` to compile and run the CLI binary with coverage collection. Tests use `t.TempDir()` to create isolated directories and `testutil.ExecOrFail()` to run shell setup scripts. Coverage from integration tests is merged with unit test coverage via `gocmdtester.WriteCombinedCoverage()`. The shared `test/gitconfig` fixture sets `init.defaultBranch = main`.

Test helpers in `test/util.go`:

- `equalLines()`: Compare multi-line strings ignoring whitespace
- `mustExecOutput()`: Run commands and return stdout
- `mockGitHubPRForBranch()`: Mock GitHub PR API responses for testing

## Requirements

- Go 1.24+
- Git 2.38+ (validated at runtime)
- GitHub CLI (`gh`) for PR operations
