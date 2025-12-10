package yascli

import (
	"fmt"

	"github.com/dansimau/yas/pkg/yas"
)

type configSetCmd struct {
	TrunkBranch             *string `description:"The name of your trunk branch, e.g. main, develop"     long:"trunk-branch"          required:"false"`
	EnableAutoPrefixBranch  bool    `description:"Enable automatic branch name prefixing with username"  long:"auto-prefix-branch"    required:"false"`
	DisableAutoPrefixBranch bool    `description:"Disable automatic branch name prefixing with username" long:"no-auto-prefix-branch" required:"false"`
	EnableWorktreeBranch    bool    `description:"Enable worktrees by default for new branches"          long:"worktree-branch"       required:"false"`
	DisableWorktreeBranch   bool    `description:"Disable worktrees by default for new branches"         long:"no-worktree-branch"    required:"false"`
}

func (c *configSetCmd) Execute(args []string) error {
	cfg := &yas.Config{
		RepoDirectory: cmd.RepoDirectory,
	}

	isConfigured, err := yas.IsConfigured(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	if isConfigured {
		_cfg, err := yas.ReadConfig(cmd.RepoDirectory)
		if err != nil {
			return NewError(err.Error())
		}

		cfg = _cfg
	}

	changed := false

	if c.TrunkBranch != nil {
		cfg.TrunkBranch = *c.TrunkBranch
		changed = true
	}

	if c.EnableAutoPrefixBranch && c.DisableAutoPrefixBranch {
		return NewError("cannot specify both --auto-prefix-branch and --no-auto-prefix-branch")
	}

	if c.EnableAutoPrefixBranch {
		cfg.AutoPrefixBranch = true
		changed = true
	}

	if c.DisableAutoPrefixBranch {
		cfg.AutoPrefixBranch = false
		changed = true
	}

	if c.EnableWorktreeBranch && c.DisableWorktreeBranch {
		return NewError("cannot specify both --worktree-branch and --no-worktree-branch")
	}

	if c.EnableWorktreeBranch {
		cfg.WorktreeBranch = true
		changed = true
	}

	if c.DisableWorktreeBranch {
		cfg.WorktreeBranch = false
		changed = true
	}

	if changed {
		if cmd.DryRun {
			fmt.Println("[DRY-RUN] Not writing config")
		} else {
			f, err := yas.WriteConfig(*cfg)
			if err != nil {
				return NewError(err.Error())
			}

			fmt.Printf("Wrote config to: %s\n", f)
		}
	}

	return nil
}
