package yas

func (yas *YAS) UpdateTrunk() error {
	// Pulling trunk relies on it tracking the remote, and trunk never has a PR
	// for refreshRemoteStatus to pick up.
	yas.ensureUpstreamTracking(yas.cfg.TrunkBranch, true)

	trunkBranchContext, err := yas.git.WithBranchContext(yas.cfg.TrunkBranch)
	if err != nil {
		return err
	}

	defer trunkBranchContext.RestoreOriginal()

	return trunkBranchContext.Pull()
}
