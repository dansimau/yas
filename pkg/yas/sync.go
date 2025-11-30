package yas

func (yas *YAS) UpdateTrunk() error {
	trunkBranchContext, err := yas.git.WithBranchContext(yas.cfg.TrunkBranch)
	if err != nil {
		return err
	}

	defer trunkBranchContext.RestoreOriginal()

	return trunkBranchContext.Pull()
}
