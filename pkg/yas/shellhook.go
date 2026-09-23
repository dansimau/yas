package yas

import "github.com/dansimau/yas/pkg/shellhook"

// ShellHook is the shell hook that lets yas change the shell's directory,
// e.g. to switch to a branch's worktree.
var ShellHook = shellhook.Hook{Command: "yas", EnvVar: "YAS_SHELL_EXEC"}
