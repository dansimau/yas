package workyardcli

import (
	"fmt"

	"github.com/dansimau/yas/pkg/shellhook"
)

// shellHook lets workyard change the shell's directory. Its variable differs
// from yas's so that yas run through "workyard exec" does not write to it.
var shellHook = shellhook.Hook{Command: "workyard", EnvVar: "WORKYARD_SHELL_EXEC"}

type hookCmd struct {
	Bash *hookShellCmd `command:"bash" description:"Print bash shell hook"`
	Zsh  *hookShellCmd `command:"zsh"  description:"Print zsh shell hook"`
}

type hookShellCmd struct {
	shell string
}

func (c *hookShellCmd) SkipYardCheck() bool {
	return true
}

func (c *hookShellCmd) Execute(args []string) error {
	fmt.Println(shellHook.Script(c.shell))

	return nil
}
