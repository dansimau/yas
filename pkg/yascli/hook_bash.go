package yascli

import (
	"fmt"

	"github.com/dansimau/yas/pkg/yas"
)

type hookBashCmd struct{}

func (c *hookBashCmd) SkipRepoCheck() bool {
	return true
}

func (c *hookBashCmd) Execute(args []string) error {
	fmt.Println(yas.ShellHook.Script("bash"))

	return nil
}
