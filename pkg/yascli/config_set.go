package yascli

import (
	"fmt"

	"github.com/dansimau/yas/pkg/yas"
)

type configSetCmd struct {
	TrunkBranch *string `long:"trunk-branch" description:"The name of your trunk branch, e.g. main, develop" required:"true"`
}

func (c *configSetCmd) Execute(args []string) error {
	yasInstance, err := yas.NewFromRepository(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	cfg := yasInstance.Config()
	changed := false

	if c.TrunkBranch != nil {
		cfg.TrunkBranch = *c.TrunkBranch
		changed = true
	}

	if changed {
		if cmd.DryRun {
			fmt.Println("[DRY-RUN] Not writing config")
		} else {
			f, err := yasInstance.UpdateConfig(cfg)
			if err != nil {
				return NewError(err.Error())
			}

			fmt.Printf("Wrote config to: %s\n", f)
		}
	}

	return nil
}
