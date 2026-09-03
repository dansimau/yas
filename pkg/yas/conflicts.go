package yas

import (
	"errors"
	"fmt"
	"os"
	"sort"
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

// ResolveConflictResolution returns the settings that would be used for a
// restack started with override, after applying the repository config and
// defaults and validating them. Unless dryRun is set it also checks the chosen
// resolver is installed; a dry run never launches it. Commands that do other
// work before restacking (e.g. sync) call this first so a bad setting fails
// before anything is touched.
func (yas *YAS) ResolveConflictResolution(override ConflictResolution, dryRun bool) (ConflictResolution, error) {
	if dryRun {
		return yas.resolveConflictResolution(override)
	}

	return yas.effectiveConflictResolution(override)
}

// effectiveConflictResolution is resolveConflictResolution plus a check that
// the chosen resolver can actually run (its binary is installed).
func (yas *YAS) effectiveConflictResolution(override ConflictResolution, fallbacks ...ConflictResolution) (ConflictResolution, error) {
	result, err := yas.resolveConflictResolution(override, fallbacks...)
	if err != nil {
		return ConflictResolution{}, err
	}

	if err := checkResolverAvailable(result); err != nil {
		return ConflictResolution{}, err
	}

	return result, nil
}

// checkResolverAvailable returns an error if res names a resolver that can't
// run in this environment. "none" is always available.
func checkResolverAvailable(res ConflictResolution) error {
	if res.Resolver == conflictresolver.None {
		return nil
	}

	resolver, err := conflictresolver.New(res.Resolver)
	if err != nil {
		return err
	}

	return resolver.CheckAvailable()
}

// resolveConflictResolution fills in unspecified fields from the fallbacks
// (in order), then the repository config, then the defaults, and validates the
// result. It does not check whether the resolver is installed: `yas continue`
// may never need to invoke it (the user may have resolved things by hand), so
// that check is deferred until a conflict actually requires the tool.
func (yas *YAS) resolveConflictResolution(override ConflictResolution, fallbacks ...ConflictResolution) (ConflictResolution, error) {
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

	// The tool is only required now that there is a conflict for it to
	// resolve (see resolveConflictResolution).
	if err := resolver.CheckAvailable(); err != nil {
		return fmt.Errorf("%w\n\nFix conflicts and run 'yas continue' to resume", err)
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

		// Snapshot the files as git left them so the resolver's work can be
		// verified afterwards. The marker length is read from the markers
		// themselves; the conflict-marker-size attribute only breaks ties.
		before, err := conflictresolver.SnapshotFiles(branch.Path(), files, branch.ConflictMarkerSize)
		if err != nil {
			return err
		}

		// Also record the rest of the working tree (including files that are
		// already dirty or ignored) so anything the resolver touches outside
		// the conflicted paths can be reported.
		statusBefore, err := branch.StatusEntries()
		if err != nil {
			return fmt.Errorf("failed to read working tree status: %w", err)
		}

		if err := resolver.Resolve(req); err != nil {
			// The tool may have edited files before failing (e.g. a session
			// dropped mid-way), so still report anything it touched outside
			// the conflict.
			msg := fmt.Sprintf("conflict resolver %s failed while rebasing %s onto %s: %v", resolver.Name(), childBranch, parentBranch, err)

			if unexpected := yas.strayChangesAfterFailure(branch, statusBefore, files); len(unexpected) > 0 {
				msg += "\n\nBefore failing it changed files outside the conflicted paths:\n  - " + strings.Join(unexpected, "\n  - ")
			}

			return fmt.Errorf("%s\n\nFix conflicts and run 'yas continue' to resume", msg)
		}

		// Marker sizes come from the snapshot: the resolver may have rewritten
		// a conflicted .gitattributes, but the markers were written before.
		remaining, err := conflictresolver.FilesWithConflictMarkers(branch.Path(), files, before)
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

		// Conflicts without textual markers (binary files, modify/delete,
		// add/add) can't be verified by the marker check. If the resolver left
		// such a file exactly as git did, staging it would silently pick one
		// side, so stop for review unless the user asked to force.
		unverifiable, err := conflictresolver.UnverifiableFiles(branch.Path(), files, before)
		if err != nil {
			return err
		}

		if len(unverifiable) > 0 {
			if res.AfterResolve != AfterResolveForce {
				return fmt.Errorf("conflict resolver %s did not change these files, whose conflicts have no textual markers (binary content or modify/delete) and so cannot be verified:\n  - %s\n\nResolve them manually (edit, 'git add' or 'git rm'), then run 'yas continue' to resume",
					resolver.Name(), strings.Join(unverifiable, "\n  - "))
			}

			fmt.Printf("Warning: unable to verify resolution of %s; continuing anyway (after-resolve=%s)\n", strings.Join(unverifiable, ", "), AfterResolveForce)
		}

		// Only the conflicted paths are staged below, so a file the resolver
		// created or edited elsewhere would be left out of the rebased commit
		// (or trip up the next rebase step). Stop so the user can decide what
		// belongs, unless forced.
		statusAfter, err := branch.StatusEntries()
		if err != nil {
			return fmt.Errorf("failed to read working tree status: %w", err)
		}

		if unexpected := unexpectedChanges(statusBefore, statusAfter, files); len(unexpected) > 0 {
			if res.AfterResolve != AfterResolveForce {
				return fmt.Errorf("conflict resolver %s changed files outside the conflicted paths:\n  - %s\n\nReview them and 'git add' any that belong to the resolution, then run 'yas continue' to resume",
					resolver.Name(), strings.Join(unexpected, "\n  - "))
			}

			fmt.Printf("Warning: %s changed files outside the conflicted paths (%s); they are left unstaged (after-resolve=%s)\n", resolver.Name(), strings.Join(unexpected, ", "), AfterResolveForce)
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

// strayChangesAfterFailure lists the paths a failed resolver changed outside
// the conflicted files. It is best effort: the resolver's own error is what
// the user needs to see, so a failure to read the status is reported on
// stderr rather than replacing it.
func (yas *YAS) strayChangesAfterFailure(branch *gitexec.BranchContext, statusBefore map[string]gitexec.StatusEntry, files []string) []string {
	statusAfter, err := branch.StatusEntries()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: unable to check for files changed by the resolver: %v\n", err)

		return nil
	}

	return unexpectedChanges(statusBefore, statusAfter, files)
}

// unexpectedChanges returns the paths whose status or working-tree fingerprint
// changed between two `git status` snapshots, excluding the conflicted files
// themselves, sorted. Comparing fingerprints (not just status codes) catches
// the resolver overwriting a file that was already untracked, modified or
// ignored before it ran.
func unexpectedChanges(before, after map[string]gitexec.StatusEntry, conflicted []string) []string {
	skip := make(map[string]bool, len(conflicted))
	for _, file := range conflicted {
		skip[file] = true
	}

	var changed []string

	for path, entry := range after {
		if skip[path] {
			continue
		}

		if prev, ok := before[path]; !ok || prev != entry {
			changed = append(changed, path)
		}
	}

	for path := range before {
		if _, ok := after[path]; !ok && !skip[path] {
			changed = append(changed, path)
		}
	}

	sort.Strings(changed)

	return changed
}
