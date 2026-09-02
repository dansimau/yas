package yascli

import (
	"fmt"
	"strings"

	"github.com/dansimau/yas/pkg/yas"
)

type configSetCmd struct {
	TrunkBranch             *string `description:"The name of your trunk branch, e.g. main, develop"                                                         long:"trunk-branch"          required:"false"`
	TrunkBranchAliases      *string `description:"Comma-separated branch names to treat as aliases for the trunk branch, e.g. master,trunk (empty to clear)" long:"trunk-branch-aliases"  required:"false"`
	EnableAutoPrefixBranch  bool    `description:"Enable automatic branch name prefixing with username"                                                      long:"auto-prefix-branch"    required:"false"`
	DisableAutoPrefixBranch bool    `description:"Disable automatic branch name prefixing with username"                                                     long:"no-auto-prefix-branch" required:"false"`
	EnableWorktreeBranch    bool    `description:"Enable worktrees by default for new branches"                                                              long:"worktree-branch"       required:"false"`
	DisableWorktreeBranch   bool    `description:"Disable worktrees by default for new branches"                                                             long:"no-worktree-branch"    required:"false"`
}

func (c *configSetCmd) Execute(args []string) error {
	if len(args) > 0 {
		return NewError("unknown argument: " + args[0])
	}

	// Load the existing config (or defaults) even if it is incomplete, so that
	// partial configs are preserved and completed rather than overwritten.
	cfg, err := yas.LoadConfig(cmd.RepoDirectory)
	if err != nil {
		return NewError(err.Error())
	}

	changed := false

	if c.TrunkBranch != nil {
		cfg.TrunkBranch = *c.TrunkBranch
		changed = true
	}

	if c.TrunkBranchAliases != nil {
		cfg.TrunkBranchAliases = parseTrunkBranchAliases(*c.TrunkBranchAliases)
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
		// Refuse to save a config that would leave the repository unusable (e.g.
		// setting aliases before a trunk branch has been configured).
		if cfg.TrunkBranch == "" {
			return NewError("trunk branch is not configured (hint: run `yas init` or pass --trunk-branch)")
		}

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

// parseTrunkBranchAliases splits a comma-separated list of branch names,
// trimming whitespace and dropping empty entries. An empty input yields nil,
// which clears the aliases.
func parseTrunkBranchAliases(value string) []string {
	var aliases []string

	for _, alias := range strings.Split(value, ",") {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}

		aliases = append(aliases, alias)
	}

	return aliases
}
