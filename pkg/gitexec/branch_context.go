package gitexec

var noop = func() error { return nil }

type BranchContext struct {
	*Repo

	restoreFn func() error
}

// RestoreOriginal restores the original branch.
func (bc *BranchContext) RestoreOriginal() error {
	return bc.restoreFn()
}

// WithBranchContext executes the specified commands from a worktree on the specified
// branch, i.e. if there is a worktree for the branch, it will be used, otherwise
// the commands will be executed in the primary repository after switching to this
// branch. When the function is complete, the original branch will be restored.
func (r *Repo) WithBranchContext(branchName string) (*BranchContext, error) {
	worktreePath, err := r.LinkedWorktreePathForBranch(branchName)
	if err != nil {
		return nil, err
	}

	if worktreePath != "" {
		return &BranchContext{
			Repo:      &Repo{path: worktreePath},
			restoreFn: noop,
		}, nil
	}

	originalBranch, err := r.GetCurrentBranchName()
	if err != nil {
		return nil, err
	}

	if err := r.QuietCheckout(branchName); err != nil {
		return nil, err
	}

	return &BranchContext{
		Repo:      r,
		restoreFn: func() error { return r.QuietCheckout(originalBranch) },
	}, nil
}
