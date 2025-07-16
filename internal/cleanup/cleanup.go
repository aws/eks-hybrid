package cleanup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// Directories to clean up when force flag is enabled
var cleanupDirs = []string{
	"/var/lib/kubelet",
	"/var/lib/cni",
	"/etc/cni/net.d",
}

// Force handles the cleanup of leftover directories.
type Force struct {
	logger  *zap.Logger
	rootDir string
}

// Option is a function that configures a Force instance.
type Option func(*Force)

// WithRootDir sets a custom root directory for testing purposes.
func WithRootDir(rootDir string) Option {
	return func(f *Force) {
		f.rootDir = rootDir
	}
}

// New creates a new Force.
func New(logger *zap.Logger, opts ...Option) *Force {
	f := &Force{
		logger:  logger,
		rootDir: "/", // Default root directory
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// Cleanup removes all configured directories.
func (c *Force) Cleanup() error {
	for _, dir := range cleanupDirs {
		fullPath := filepath.Join(c.rootDir, strings.TrimPrefix(dir, "/"))
		if err := c.removeDir(fullPath); err != nil {
			return fmt.Errorf("removing directory %s: %w", dir, err)
		}
	}
	return nil
}

func (c *Force) removeDir(dir string) error {
	// Check if /etc/passwd file is present
	passwdFile := "/etc/passwd"
	if _, err := os.Stat(passwdFile); os.IsNotExist(err) {
		c.logger.Warn("Before /etc/passwd file does not exist", zap.String("path", passwdFile))
	} else if err != nil {
		c.logger.Error("Before Error checking /etc/passwd file status", zap.String("path", passwdFile), zap.Error(err))
	} else {
		c.logger.Info("Before /etc/passwd file is present", zap.String("path", passwdFile))
	}

	// Check if directory exists before attempting to remove
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		c.logger.Info("Directory does not exist, skipping removal", zap.String("path", dir))
		return nil
	} else if err != nil {
		c.logger.Error("Error checking directory status", zap.String("path", dir), zap.Error(err))
		return err
	}

	c.logger.Info("--- SAIB Removing directory (force cleanup)", zap.String("path", dir))
	if err := os.RemoveAll(dir); err != nil {
		c.logger.Error("--- SAIB Failed to remove directory", zap.String("path", dir), zap.Error(err))
		return err
	}
	c.logger.Info("Successfully removed directory", zap.String("path", dir))

	if _, err := os.Stat(passwdFile); os.IsNotExist(err) {
		c.logger.Warn("After /etc/passwd file does not exist", zap.String("path", passwdFile))
	} else if err != nil {
		c.logger.Error("After Error checking /etc/passwd file status", zap.String("path", passwdFile), zap.Error(err))
	} else {
		c.logger.Info("After /etc/passwd file is present", zap.String("path", passwdFile))
	}
	return nil
}
