package workyard

import (
	"golang.org/x/sys/unix"
)

// darwinCloner clones with clonefile(2), which on APFS copies a whole
// directory tree (or a file) in one atomic, copy-on-write syscall. Both paths
// must be on the same volume.
type darwinCloner struct{}

func newCloner() cloner {
	return darwinCloner{}
}

func (darwinCloner) CloneTree(src, dst string) error {
	// CLONE_NOFOLLOW clones a symlink itself rather than its target;
	// CLONE_NOOWNERCOPY gives the clone to the current user rather than
	// copying ownership, which would need privileges.
	return unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW|unix.CLONE_NOOWNERCOPY)
}
