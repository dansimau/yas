package yas

import (
	"fmt"
	"os"
)

// ResolveTrunkAlias returns the configured trunk branch name if branchName
// exactly matches one of the configured trunk branch aliases. Otherwise it
// returns branchName unchanged.
//
// The first time a replacement happens for this YAS instance (i.e. once per
// invocation), an informational message is printed to stderr.
func (yas *YAS) ResolveTrunkAlias(branchName string) string {
	if branchName == "" || branchName == yas.cfg.TrunkBranch {
		return branchName
	}

	for _, alias := range yas.cfg.TrunkBranchAliases {
		if alias != branchName {
			continue
		}

		if !yas.trunkAliasNoticeShown {
			yas.trunkAliasNoticeShown = true

			// Color code 6 (cyan) for informational output
			fmt.Fprintf(os.Stderr, "\x1b[36mReplaced branch name from '%s' to '%s' (trunk branch alias)\x1b[0m\n", branchName, yas.cfg.TrunkBranch)
		}

		return yas.cfg.TrunkBranch
	}

	return branchName
}

// ResolveTrunkAliases applies ResolveTrunkAlias to every name in branchNames
// and returns the resulting slice.
func (yas *YAS) ResolveTrunkAliases(branchNames []string) []string {
	if branchNames == nil {
		return nil
	}

	resolved := make([]string, 0, len(branchNames))
	for _, name := range branchNames {
		resolved = append(resolved, yas.ResolveTrunkAlias(name))
	}

	return resolved
}
