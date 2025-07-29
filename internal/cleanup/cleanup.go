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
	c.logger.Info("Removing directory", zap.String("path", dir))
	return os.RemoveAll(dir)
}
