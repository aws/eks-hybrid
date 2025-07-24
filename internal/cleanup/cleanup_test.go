package cleanup_test

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"go.uber.org/zap/zaptest"

	"github.com/aws/eks-hybrid/internal/cleanup"
)

func TestCleanup(t *testing.T) {
	tests := []struct {
		name        string
		setupDirs   []string
		expectError string
	}{
		{
			name: "cleanup existing directories",
			setupDirs: []string{
				"/var/lib/cni",
				"/etc/cni/net.d",
			},
		},
		{
			name:      "cleanup non-existent directories",
			setupDirs: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			tmpRoot := t.TempDir()

			for _, dir := range tt.setupDirs {
				fullPath := filepath.Join(tmpRoot, dir)
				g.Expect(os.MkdirAll(fullPath, 0o755)).To(Succeed(), "Failed to create test directory %s", fullPath)
				testFile := filepath.Join(fullPath, "test.txt")
				g.Expect(os.WriteFile(testFile, []byte("test"), 0o644)).To(Succeed(), "Failed to create test file %s", testFile)
			}

			logger := zaptest.NewLogger(t)
			force := cleanup.New(logger, cleanup.WithRootDir(tmpRoot))
			err := force.Cleanup()

			if tt.expectError == "" {
				g.Expect(err).ToNot(HaveOccurred(), "Unexpected error occurred")
			} else {
				g.Expect(err).To(HaveOccurred(), "Expected error but got none")
				g.Expect(err).To(MatchError(ContainSubstring(tt.expectError), "Error message does not match expected"))
			}

			for _, dir := range tt.setupDirs {
				fullPath := filepath.Join(tmpRoot, dir)
				_, err := os.Stat(fullPath)
				g.Expect(os.IsNotExist(err)).To(BeTrue(), "Directory %s should not exist", fullPath)
			}
		})
	}
}
