package yascli

import (
	"errors"
	"fmt"

	"github.com/dansimau/yas/pkg/cliutil"
	"github.com/dansimau/yas/pkg/gitexec"
	"github.com/dansimau/yas/pkg/yas"
)

type initCmd struct{}

func (c *initCmd) Execute(args []string) error {
	isNewConfig := !yas.IsConfigured(cmd.RepoDirectory)

	cfg := &yas.Config{
		RepoDirectory:    cmd.RepoDirectory,
		AutoPrefixBranch: true,
	}

	if !isNewConfig {
		_cfg, err := yas.ReadConfig(cmd.RepoDirectory)
		if err != nil {
			return NewError(err.Error())
		}

		cfg = _cfg
	}

	// If no trunk branch is already configured, try to auto-detect it
	if cfg.TrunkBranch == "" {
		repo := gitexec.WithRepo(cmd.RepoDirectory)
		if detectedBranch, err := repo.DetectMainBranch(); err == nil && detectedBranch != "" {
			cfg.TrunkBranch = detectedBranch
		}
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
