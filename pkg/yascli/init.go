package yascli

import (
	"errors"
	"fmt"

	"github.com/dansimau/yas/pkg/cliutil"
	"github.com/dansimau/yas/pkg/yas"
)

type initCmd struct{}

func (c *initCmd) Execute(args []string) error {
	cfg := &yas.Config{
		RepoDirectory: cmd.RepoDirectory,
	}

	if yas.IsConfigured(cmd.RepoDirectory) {
		_cfg, err := yas.ReadConfig(cmd.RepoDirectory)
		if err != nil {
			return NewError(err.Error())
		}

		cfg = _cfg
	}

	cfg.TrunkBranch = cliutil.Prompt(cliutil.PromptOptions{
		Text:    "What is your trunk branch name?",
		Default: cfg.TrunkBranch,
		Validator: func(input string) error {
			if input == "" {
				return errors.New("branch name cannot be empty")
			}

			return nil
		},
	})

	dest, err := yas.WriteConfig(*cfg)
	if err != nil {
		return NewError(err.Error())
	}

	fmt.Printf("Saved config to: %s\n", dest)

	return nil
}
