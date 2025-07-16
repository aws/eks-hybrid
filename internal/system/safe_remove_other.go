//go:build !linux

package system

import (
	"fmt"
)

// platformForceUnmount performs a force unmount for non-Linux platforms
func (sr *SafeRemover) platformForceUnmount(mountPoint string) error {
	// On non-Linux platforms, we don't have the same syscall constants
	// Fall back to using the mount interface only
	return fmt.Errorf("force unmount not supported on this platform")
}