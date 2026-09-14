//go:build !darwin

package workyard

// noCloner is used on platforms without a clone syscall; every copy falls
// back to a plain copy.
type noCloner struct{}

func newCloner() cloner {
	return noCloner{}
}

func (noCloner) CloneTree(string, string) error {
	return errCloneUnsupported
}
