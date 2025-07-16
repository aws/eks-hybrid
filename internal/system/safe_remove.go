package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	mountutils "k8s.io/mount-utils"
)

// SafeRemover provides safe removal capabilities with unmount support
type SafeRemover struct {
	mounter mountutils.Interface
}

// NewSafeRemover creates a new SafeRemover instance
func NewSafeRemover() *SafeRemover {
	return &SafeRemover{
		mounter: mountutils.New(""),
	}
}

// SafeRemoveAll safely removes a directory, optionally handling mount points
// allowUnmount: if true, attempts to unmount any mount points found
// forceUnmount: if true, uses force unmount when graceful unmount fails
func SafeRemoveAll(path string, allowUnmount bool, forceUnmount bool) error {
	remover := NewSafeRemover()
	return remover.SafeRemoveAll(path, allowUnmount, forceUnmount)
}

// SafeRemoveAll is the main entry point that handles both modes
func (sr *SafeRemover) SafeRemoveAll(path string, allowUnmount bool, forceUnmount bool) error {
	// Clean and get absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Find all mount points within the target path
	mountPoints, err := sr.findMountPointsInPath(absPath)
	if err != nil {
		// If mount utilities are not supported on this platform, fall back to regular removal
		if strings.Contains(err.Error(), "not supported") {
			return os.RemoveAll(absPath)
		}
		return fmt.Errorf("failed to find mount points: %w", err)
	}

	if len(mountPoints) == 0 {
		return os.RemoveAll(absPath)
	}

	// Decide action based on allowUnmount flag
	if !allowUnmount {
		// Safe mode: refuse to delete if mount points exist
		return fmt.Errorf("cannot delete %s: contains %d mount points %v (mount points detected)", 
			absPath, len(mountPoints), mountPoints)
	}

	// Unmount mode: attempt to unmount all found mount points
	return sr.unmountAndRemove(absPath, mountPoints, forceUnmount)
}

// findMountPointsInPath finds all mount points within the given path
func (sr *SafeRemover) findMountPointsInPath(targetPath string) ([]string, error) {
	var mountPoints []string

	// Walk through the directory tree to find mount points
	err := filepath.WalkDir(targetPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Skip inaccessible paths but continue walking
			return nil
		}

		// Check if this path (file or directory) is a mount point
		isMounted, err := sr.mounter.IsMountPoint(path)
		if err != nil {
			// Skip paths we can't check but continue walking
			return nil
		}

		if isMounted {
			mountPoints = append(mountPoints, path)
			// Skip walking into directory mount points since they're separate filesystems
			// This is an optimization - we don't need to check subdirectories of mount points
			// For file mount points, there are no subdirectories to skip
			if d.IsDir() && path != targetPath {
				return filepath.SkipDir
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory tree: %w", err)
	}

	return mountPoints, nil
}

// unmountAndRemove handles the unmount and removal process
func (sr *SafeRemover) unmountAndRemove(targetPath string, mountPoints []string, forceUnmount bool) error {
	// Sort mount points by depth (deepest first for unmounting)
	sortedMounts := sr.sortMountPointsByDepth(mountPoints)

	// Attempt to unmount all found mount points
	for _, mountPoint := range sortedMounts {
		if err := sr.unmountWithRetry(mountPoint, forceUnmount); err != nil {
			return fmt.Errorf("failed to unmount %s: %w", mountPoint, err)
		}
	}

	// Wait for unmount operations to complete
	time.Sleep(200 * time.Millisecond)

	// Verify all mount points are gone
	if err := sr.verifyUnmounted(mountPoints); err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	// Now safe to remove the directory
	return os.RemoveAll(targetPath)
}

// sortMountPointsByDepth sorts mount points by depth (deepest first)
func (sr *SafeRemover) sortMountPointsByDepth(mountPoints []string) []string {
	// Create a copy of the slice
	sorted := make([]string, 0, len(mountPoints))
	sorted = append(sorted, mountPoints...)
	
	// Simple bubble sort by path depth (number of separators)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			depthI := strings.Count(sorted[i], string(filepath.Separator))
			depthJ := strings.Count(sorted[j], string(filepath.Separator))
			
			// Sort deepest first
			if depthI < depthJ {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	
	return sorted
}

// unmountWithRetry attempts to unmount a path with retry logic
func (sr *SafeRemover) unmountWithRetry(mountPoint string, forceUnmount bool) error {
	maxRetries := 3
	
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Try graceful unmount first
		err := sr.mounter.Unmount(mountPoint)
		if err == nil {
			return nil
		}

		// If graceful unmount fails and force is enabled, try force unmount
		if forceUnmount {
			if err := sr.platformForceUnmount(mountPoint); err == nil {
				return nil
			}
		}

		// Wait before retry (except on last attempt)
		if attempt < maxRetries {
			sleepDuration := time.Duration(attempt) * time.Second
			time.Sleep(sleepDuration)
		}
	}

	return fmt.Errorf("failed to unmount after %d attempts", maxRetries)
}



// verifyUnmounted checks that all mount points have been successfully unmounted
func (sr *SafeRemover) verifyUnmounted(mountPoints []string) error {
	for _, mountPoint := range mountPoints {
		isMounted, err := sr.mounter.IsMountPoint(mountPoint)
		if err != nil {
			// If we can't check, continue but log the issue
			continue
		}
		if isMounted {
			return fmt.Errorf("mount point %s is still mounted after unmount attempt", mountPoint)
		}
	}
	
	return nil
}