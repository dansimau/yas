package yas

import (
	"fmt"
	"os"
)

func (yas *YAS) UpdateTrunk() error {
	if err := yas.git.QuietCheckout(yas.cfg.TrunkBranch); err != nil {
		return err
	}

	// Switch back to original branch
	defer func() {
		if err := yas.git.QuietCheckout("-"); err != nil {
			fmt.Fprintf(os.Stderr, "failed to checkout original branch: %v\n", err)
		}
	}()

	return yas.git.Pull()
}
