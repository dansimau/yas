# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

yas (Yet Another Stacked Diff Tool) is a CLI tool for managing stacked PRs on GitHub, written in Go. It enables developers to create and maintain hierarchical branches where each branch depends on its parent, forming a stack of changes.

## Build, Test & Lint Commands

```bash
# Build the binary
go build ./cmd/yas

# Run all tests and see failures
make test >/dev/null

# Run tests in a specific package
go test ./pkg/yas
go test ./test

# Run a specific test
go test ./test -run TestUpdateTrunk

# Run tests with verbose output
make test

# Run lint checks
make lint
```

## Architecture

### Core Package Structure

- **cmd/yas/main.go**: Entry point that delegates to `pkg/yascli`
- **pkg/yascli**: CLI command handlers and argument parsing using go-flags
- **pkg/yas**: Core business logic for stacked diff management
- **pkg/gitexec**: Git operations wrapper using go-git and command execution
- **pkg/xexec**: Command execution utilities with environment control
- **test/**: Integration tests using temporary git repositories

### State Management

yas maintains two key files in the `.git` directory:
- **.git/yas.yaml**: Configuration (trunk branch name)
- **.git/.yasstate**: JSON database tracking branch metadata and parent relationships

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

**Sync** (`yas sync`):
- Tracks untracked branches by refreshing remote status
- Queries GitHub PR status using `gh pr list --json`
- Deletes local branches for merged PRs
- Updates trunk branch with `git pull --ff --ff-only`

### Environment Management

Git operations use cleaned environments (`CleanedGitEnv()` in pkg/gitexec/util.go) to avoid inheriting unwanted git configuration from the parent process.

### Testing Patterns

Integration tests (in `test/`) use `testutil.WithTempWorkingDir()` to create isolated git repositories. Tests execute git commands directly to set up scenarios, then verify behavior using `yascli.Run()` and git command output.

## Requirements

- Go 1.22+
- Git 2.38+ (validated at runtime)
- GitHub CLI (`gh`) for PR operations
