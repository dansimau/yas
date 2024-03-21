package yascli

import "github.com/davecgh/go-spew/spew"

type configShowCmd struct{}

func (c *configShowCmd) Execute(args []string) error {
	spew.Dump(cmd.yas.Config())
	return nil
}
