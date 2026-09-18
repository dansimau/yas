# yas (Yet Another Stacked Diff Tool)

![checks](https://github.com/dansimau/yas/actions/workflows/checks.yaml/badge.svg)
![coverage](https://raw.githubusercontent.com/dansimau/yas/badges/.badges/main/coverage.svg)

yas is a CLI tool for managing stacked PRs on GitHub. Each branch in a stack
builds on its parent, so you can ship a large change as a chain of small,
reviewable PRs while yas keeps the branches, rebases and PR bases in sync.

## Features

- ⽙ **Stacks as a tree** — `yas list` shows every branch, its parent, its PR link and whether it needs a restack or resubmit
- 🗂️ **Worktree aware** — branches can live in separate worktrees, and the shell hook changes directory when you switch
- ↩️ **Painless rebases** — `yas restack` automatically rebases all branches in the stack
- ✨ **Auto-resolve conflicts** – use Claude to automatically resolve conflicts during rebase
- 🚀 **One-command PR submit** — `yas submit` pushes and opens or updates the PR
- 📝 **PR annotations** — every submitted PR gets a summary of the stack it belongs to, so reviewers can see where it fits
- 🧹 **Sync after merges** — `yas sync` pulls latest trunk, refreshes PR status and deletes local branches whose PRs merged
- ✅ **Review and CI at a glance** — `yas list --status` shows review decision and CI state next to each PR
- 🔀 **Fast branch switching** — `yas branch` checks out by name or opens an interactive picker, and hops between worktrees for you
- 🧭 **Restructure freely** — `yas move` reparents a branch and its descendants; `yas add` adopts existing branches into a stack

## Installation

Requires Go 1.24+, Git 2.38+ and the [GitHub CLI](https://cli.github.com/) (`gh`), logged in.

```sh
go install github.com/dansimau/yas/cmd/yas@latest
```

Install the shell hook so `yas branch` can change directory when
switching between worktrees. Add to `~/.zshrc` or `~/.bashrc`:

```sh
eval "$(yas hook zsh)"    # or: eval "$(yas hook bash)"
```

## Quick start

### 1. See your stacks

```console
$ yas ls --status
main
├── auth-refactor [https://github.com/acme/app/pull/101] (review: ✅, CI: ✅)
│   └── auth-tests (needs restack) [https://github.com/acme/app/pull/102] (review: ❌, CI: ⏳) *
└── fix-typo (not submitted)
```

Each branch shows its PR link, whether it `needs restack` or `needs submit`, and with `--status` the PR review and CI state. The `*` marks the branch you are on.

### 2. Switch branches

```sh
yas branch auth-refactor   # or: yas br auth-refactor
yas branch                 # no argument opens an interactive picker
```

If the target branch lives in another worktree, yas changes directory for you automatically.

### 3. Create a new branch

```sh
yas branch add-rate-limiting
```

This creates `add-rate-limiting` on top of the current branch and records the parent relationship. Use `--parent <branch>` to stack on something else, or `--worktree` to check it out in a new worktree.

### 4. Submit a draft PR

```sh
git commit -am "Add rate limiting"
yas submit --draft
```

yas pushes the branch, opens a draft PR with its base set to the parent
branch, and annotates the PR description with the stack. Run `yas submit`
again after more commits to update it.

### 5. Merge a PR

From the PR branch:

```console
$ yas merge
```

### 6. Sync after a merge

Once a PR in the stack is merged on GitHub:

```console
$ yas sync
🧹 Checking for merged PRs...
Deleted branch 'auth-refactor'
🔄 Pulling main...
```

Merged branches are removed locally, their children are reparented onto
trunk, and trunk is fast-forwarded to the remote.

### 7. Restack and resubmit

After trunk moves (a sync, or a merge elsewhere), rebase the stack and push the rebased branches back to their PRs:

```sh
yas restack --all          # or `yas restack` for the current stack only
yas submit --outdated      # resubmit only branches whose PRs are behind
```

`yas submit --stack` resubmits every branch in the current stack. If a rebase hits conflicts, resolve them and run `yas continue`, or back out
with `yas abort`.

Use `yas sync --restack` to do the sync and restack everything in one step.

## Advanced Features

### Auto resolve conflicts during rebase

yas can hand rebase conflicts to an external tool instead of stopping. Currently
[Claude Code](https://docs.anthropic.com/en/docs/claude-code) is supported (it
runs `claude -p ...` in the conflicted worktree, restricted to editing files):

```sh
yas config set --conflict-resolver=claude          # default: none
yas config set --after-resolve=continue            # default: stop
```

By default, yas stops so you can verify the resolution. Use `--after-resolve=continue` to automatically continue 🤞

If the tool fails or leaves markers behind, yas stops exactly as it would for a manual conflict, and `yas continue` / `yas abort` work as usual.

## Commands

| Command | Description |
| --- | --- |
| `yas init` | Set up yas in the current repository |
| `yas list` (`ls`) | List stacks; `--status`, `--all`, `--current-stack` |
| `yas branch` (`br`) | Switch to a branch |
| `yas branch <name>` (`br`) | Switch to the named branch, creating it if it doesn't exist |
| `yas add [branch]` | Track an existing branch and set its parent |
| `yas move` | Move the current branch and its descendants to a new parent |
| `yas submit` | Push and open/update PRs |
| `yas restack [branch]` | Rebase all branches in the current stack or `--all` for all branches; `--conflict-resolver`, `--after-resolve` |
| `yas continue` / `yas abort` | Resume or abort a restack after conflicts |
| `yas sync` | Pull trunk, refresh PR status, delete merged branches |
| `yas merge` | Merge the PR for the current branch |
| `yas delete` | Delete a branch and its worktree |
| `yas config` | Show or set repository configuration |
| `yas hook <bash\|zsh>` | Print the shell integration hook |

## workyard

The repository also ships `workyard`, a separate tool for working on a branch across a whole
directory tree of repositories. A workyard is a fast copy of a directory (say, your multi-repo
workspace) in which every git repository is replaced by a `git worktree` of the original, checked
out at the branch you choose. On macOS (APFS) everything that is not a repository is cloned
copy-on-write, so a workspace of many repos and gigabytes of dependencies is ready in seconds.

```sh
go install github.com/dansimau/yas/cmd/workyard@latest
```

```console
$ cd ~/code/workspace
$ workyard create --branch feature-x ../workspace-feature-x
Created workyard in 0.42s

$ cd ../workspace-feature-x/web
$ workyard st                       # git status --short --branch in every repo
$ workyard diff --stat              # git diff --stat in every repo
$ workyard git log --oneline -3     # any git command
$ workyard ls
$ cd .. && workyard remove workspace-feature-x
```

Like a git worktree, a workyard only holds a pointer back to where it came from (the `.workyard`
file at its root); the source keeps the bookkeeping under `.workyard/`. The source itself must not
be a git repository (for that, use `git worktree`) or another workyard.

When the branch does not exist in a repository it is created from that repository's trunk (`main`
or `master`, or whatever `.workyard/config.yaml` says). A branch that exists only on a remote is
created to track it; a tag or commit is checked out detached; a branch that is already checked out
in the source is an error.

| Command | Description |
| --- | --- |
| `workyard create [--source DIR] [--branch NAME] TARGET` | Create a workyard; `--dry-run` shows the plan |
| `workyard status` (`st`) `[git args]` | `git status --short --branch` in every repo (arguments replace the defaults) |
| `workyard diff [git args]` | `git diff` in every repo |
| `workyard git <args>` | Any git command in every repo; exit code 1 if it failed anywhere |
| `workyard list` (`ls`) `[--json]` | Repositories with branch, commit and a `*` when dirty |
| `workyard remove [PATH] [-f] [--yes]` | Remove the worktrees (dirty ones only with `-f`, locked with `-f -f`), the directory, and the branches workyard created (unmerged ones only with `-f`) |

The fan-out commands print a `==> path (branch)` header for each repository that produced output.
`--unordered` prints results as they finish, `-p`/`--parallelism` and `--color` control concurrency
and color. Run them from anywhere inside the workyard, or set `WORKYARD_ROOT`.

Optional source configuration in `<source>/.workyard/config.yaml`:

| Key | Description |
| --- | --- |
| `trunk` | Branch to create new branches from when the requested branch does not exist (default: autodetect `main`, then `master`) |
| `repos.<path>.trunk` | Per-repository override, keyed by path relative to the source |

Things to know: files ignored by git (`node_modules`, build output) are not copied into worktrees;
submodules are not initialised (`-v` reports both); if you move a workyard with `mv`, run
`git worktree repair` in each repository; the copy-on-write fast path needs source and target on
the same APFS volume, otherwise files are copied.

## Configuration

Configuration lives in `.yas/yas.yaml` and is managed with `yas config set`:

| Option | Description |
| --- | --- |
| `--trunk-branch=<name>` | The trunk branch, e.g. `main` |
| `--trunk-branch-aliases=<a,b>` | Comma-separated branch names that are treated as the trunk branch when passed as arguments, e.g. `master,trunk`. Useful when switching between repos with different trunk names. When an alias is replaced, yas prints a note (once per invocation). Pass an empty value to clear. |
| `--auto-prefix-branch` / `--no-auto-prefix-branch` | Prefix new branch names with your username |
| `--worktree-branch` / `--no-worktree-branch` | Create new branches in worktrees by default |
