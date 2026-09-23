package yascli

import (
	"fmt"

	"github.com/dansimau/yas/pkg/yas"
)

type hookZshCmd struct{}

func (c *hookZshCmd) SkipRepoCheck() bool {
	return true
}

func (c *hookZshCmd) Execute(args []string) error {
	fmt.Println(yas.ShellHook.Script("zsh"))

	return nil
}
