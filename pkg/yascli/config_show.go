package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
	"github.com/davecgh/go-spew/spew"
)

type configShowCmd struct{}

func (c *configShowCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	spew.Dump(yasInstance.Config())
	return nil
}
