package yas

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dansimau/yas/pkg/conflictresolver"
	"github.com/dansimau/yas/pkg/gitexec"
)

// Valid values for the after-resolve setting.
const (
	// AfterResolveStop pauses the restack after the resolver has run so the
	// user can review the result before running `yas continue`.
	AfterResolveStop = "stop"
	// AfterResolveContinue resumes the rebase automatically, provided the
	// resolver left no conflict markers behind.
	AfterResolveContinue = "continue"
	// AfterResolveForce resumes the rebase even if conflict markers remain.
	AfterResolveForce = "force"
)

// Defaults for the conflict resolution settings.
const (
	DefaultConflictResolver = conflictresolver.None
	DefaultAfterResolve     = AfterResolveStop
)

// AfterResolveValues lists the valid after-resolve settings.
var AfterResolveValues = []string{AfterResolveStop, AfterResolveContinue, AfterResolveForce}

// ConflictResolution describes how rebase conflicts should be handled. Empty
// fields mean "not specified" and fall back to the saved restack state (for
// `yas continue`) and then the repository config.
type ConflictResolution struct {
	Resolver     string
	AfterResolve string
}

// ValidateConflictResolution returns an error if either field holds a value
// that isn't recognised. Empty fields are allowed (they mean "unspecified").
func ValidateConflictResolution(res ConflictResolution) error {
	if res.Resolver != "" && !conflictresolver.IsValid(res.Resolver) {
		return fmt.Errorf("invalid conflict-resolver %q (valid values: %s)", res.Resolver, strings.Join(conflictresolver.Names(), ", "))
	}

	if res.AfterResolve != "" && !isValidAfterResolve(res.AfterResolve) {
		return fmt.Errorf("invalid after-resolve %q (valid values: %s)", res.AfterResolve, strings.Join(AfterResolveValues, ", "))
	}

	return nil
}

func isValidAfterResolve(value string) bool {
	for _, valid := range AfterResolveValues {
		if value == valid {
			return true
		}
	}

	return false
}

// effectiveConflictResolution fills in unspecified fields from the fallbacks
// (in order) and finally the repository config, validates the result and
// checks the chosen resolver is usable.
func (yas *YAS) effectiveConflictResolution(override ConflictResolution, fallbacks ...ConflictResolution) (ConflictResolution, error) {
	result := override

	candidates := make([]ConflictResolution, 0, len(fallbacks)+2)
	candidates = append(candidates, fallbacks...)
	candidates = append(candidates,
		ConflictResolution{Resolver: yas.cfg.ConflictResolver, AfterResolve: yas.cfg.AfterResolve},
		ConflictResolution{Resolver: DefaultConflictResolver, AfterResolve: DefaultAfterResolve},
	)

	for _, candidate := range candidates {
		if result.Resolver == "" {
			result.Resolver = candidate.Resolver
		}

		if result.AfterResolve == "" {
			result.AfterResolve = candidate.AfterResolve
		}
	}

	if err := ValidateConflictResolution(result); err != nil {
		return ConflictResolution{}, err
	}

	if result.Resolver != conflictresolver.None {
		resolver, err := conflictresolver.New(result.Resolver)
		if err != nil {
			return ConflictResolution{}, err
		}

		if err := resolver.CheckAvailable(); err != nil {
			return ConflictResolution{}, err
		}
	}

	return result, nil
}

// errManualResolutionRequired builds the error returned when a rebase has
// stopped on conflicts that yas will not (or could not) resolve itself.
func errManualResolutionRequired(childBranch, parentBranch string, cause error) error {
	return fmt.Errorf("rebase failed for %s onto %s: %w\n\nFix conflicts and run 'yas continue' to resume", childBranch, parentBranch, cause)
}

// handleRebaseConflict is called when a rebase of childBranch onto
// parentBranch has stopped with conflicts (the restack state has already been
// saved). Depending on res it either returns an error asking the user to fix
// the conflicts, or runs the configured resolver and then stops or continues
// the rebase.
//
// It returns nil only when the rebase has been driven to completion.
func (yas *YAS) handleRebaseConflict(branch *gitexec.BranchContext, res ConflictResolution, childBranch, parentBranch string, cause error) error {
	if res.Resolver == conflictresolver.None {
		return errManualResolutionRequired(childBranch, parentBranch, cause)
	}

	resolver, err := conflictresolver.New(res.Resolver)
	if err != nil {
		return err
	}

	for {
		files, err := branch.UnmergedFiles()
		if err != nil {
			return fmt.Errorf("failed to list conflicted files: %w", err)
		}

		if len(files) == 0 {
			// The rebase stopped for a reason other than content conflicts
			// (nothing for a resolver to do).
			return errManualResolutionRequired(childBranch, parentBranch, cause)
		}

		fmt.Printf("\nConflicts in %d file(s) while rebasing %s onto %s; resolving with %s...\n", len(files), childBranch, parentBranch, resolver.Name())

		for _, file := range files {
			fmt.Printf("  - %s\n", file)
		}

		req := conflictresolver.Request{
			Dir:    branch.Path(),
			Files:  files,
			Branch: childBranch,
			Onto:   parentBranch,
		}

		// Best effort: the subject helps the resolver understand intent, but
		// its absence shouldn't block resolution.
		if subject, err := branch.GetCommitSubject("REBASE_HEAD"); err == nil {
			req.CommitSubject = subject
		}

		if err := resolver.Resolve(req); err != nil {
			return fmt.Errorf("conflict resolver %s failed while rebasing %s onto %s: %w\n\nFix conflicts and run 'yas continue' to resume", resolver.Name(), childBranch, parentBranch, err)
		}

		// Marker length can be customised per path via .gitattributes, so ask
		// git rather than assuming the default.
		remaining, err := conflictresolver.FilesWithConflictMarkers(branch.Path(), files, branch.ConflictMarkerSize)
		if err != nil {
			return err
		}

		if len(remaining) > 0 {
			if res.AfterResolve != AfterResolveForce {
				return fmt.Errorf("conflict resolver %s left conflict markers in:\n  - %s\n\nFix conflicts and run 'yas continue' to resume",
					resolver.Name(), strings.Join(remaining, "\n  - "))
			}

			fmt.Printf("Warning: conflict markers remain in %s; continuing anyway (after-resolve=%s)\n", strings.Join(remaining, ", "), AfterResolveForce)
		}

		if res.AfterResolve == AfterResolveStop {
			return fmt.Errorf("conflicts in %s were resolved by %s while rebasing %s onto %s\n\nReview the changes (e.g. 'git diff'), then stage them with 'git add' and run 'yas continue' to resume, or run 'yas abort' to cancel",
				strings.Join(files, ", "), resolver.Name(), childBranch, parentBranch)
		}

		if err := branch.Add(files...); err != nil {
			return fmt.Errorf("failed to stage resolved files: %w", err)
		}

		fmt.Printf("Continuing rebase for %s...\n", childBranch)

		continueErr := branch.RebaseContinue()

		stillInProgress, err := branch.IsRebaseInProgress()
		if err != nil {
			return fmt.Errorf("failed to check if rebase is in progress: %w", err)
		}

		if !stillInProgress {
			if continueErr != nil {
				return fmt.Errorf("rebase continue failed for %s: %w", childBranch, continueErr)
			}

			return nil
		}

		if continueErr == nil {
			// git stopped without an error but the rebase isn't finished; this
			// isn't a conflict, so hand back to the user.
			return errors.New("rebase for " + childBranch + " stopped unexpectedly\n\nRun 'yas continue' to resume")
		}

		// Another commit in the same rebase hit conflicts; go round again.
		cause = continueErr
	}
}
