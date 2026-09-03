package yascli

import (
	"github.com/dansimau/yas/pkg/yas"
)

// conflictResolutionFlags are shared by every command that can rebase
// branches. Embed the struct in a command to add the flags. Unset flags fall
// back to the repository config (`yas config set --conflict-resolver=...`).
type conflictResolutionFlags struct {
	ConflictResolver string `description:"Tool used to automatically resolve rebase conflicts, e.g. claude or none (default: from config, or none)" long:"conflict-resolver"`
	AfterResolve     string `choice:"stop"                                                                                                          choice:"continue"        choice:"force" description:"What to do after the conflict resolver runs: stop for review, continue if no conflict markers remain, or force to continue regardless (default: from config, or stop)" long:"after-resolve"`
}

func (f conflictResolutionFlags) conflictResolution() yas.ConflictResolution {
	return yas.ConflictResolution{
		Resolver:     f.ConflictResolver,
		AfterResolve: f.AfterResolve,
	}
}
