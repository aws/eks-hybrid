//go:build linux

package system

import (
	"fmt"
	"syscall"
)

// platformForceUnmount performs a force unmount using Linux-specific system calls
func (sr *SafeRemover) platformForceUnmount(mountPoint string) error {
	// Try lazy unmount first (MNT_DETACH) - safer option
	if err := syscall.Unmount(mountPoint, syscall.MNT_DETACH); err == nil {
		return nil
	}

	// Try force unmount (MNT_FORCE) - more aggressive
	if err := syscall.Unmount(mountPoint, syscall.MNT_FORCE); err == nil {
		return nil
	}

	// Try both - force + detach
	if err := syscall.Unmount(mountPoint, syscall.MNT_FORCE|syscall.MNT_DETACH); err == nil {
		return nil
	}

	return fmt.Errorf("all unmount methods failed")
}