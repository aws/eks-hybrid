package kubelet_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/test"
	"github.com/aws/eks-hybrid/internal/tracker"
)

func TestUninstall(t *testing.T) {
	tests := []struct {
		name                 string
		makeReadOnly         string // path to make read-only to simulate deletion failure
		noCurrentKubeletCert bool
		wantErr              string
	}{
		{
			name: "successful uninstallation",
		},
		{
			name:                 "no kubelet cert",
			noCurrentKubeletCert: true,
		},
		{
			name:         "partial failure - one file fails to delete",
			makeReadOnly: kubelet.BinPath,
			wantErr:      "permission denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			tmpDir := t.TempDir()

			// Create test files
			actualCertFile := "/var/lib/kubelet/pki/kubelet-server-2024-01-01.pem"
			currentCertFile := "/var/lib/kubelet/pki/kubelet-server-current.pem"
			setupFiles := []string{
				kubelet.BinPath,
				kubelet.UnitPath,
				"/var/lib/kubelet/kubeconfig",
				"/etc/kubernetes/kubelet/config.json",
			}

			if !tt.noCurrentKubeletCert {
				setupFiles = append(setupFiles, actualCertFile)
			}

			for _, file := range setupFiles {
				fullPath := filepath.Join(tmpDir, file)
				err := os.MkdirAll(filepath.Dir(fullPath), 0o755)
				g.Expect(err).NotTo(HaveOccurred())
				err = os.WriteFile(fullPath, []byte("test"), 0o644)
				g.Expect(err).NotTo(HaveOccurred())

				// If this is the file we want to make read-only
				if file == tt.makeReadOnly {
					// Make parent directory read-only to prevent deletion
					err = os.Chmod(filepath.Dir(fullPath), 0o555)
					g.Expect(err).NotTo(HaveOccurred())
				}
			}

			if !tt.noCurrentKubeletCert {
				g.Expect(os.Symlink(filepath.Join(tmpDir, actualCertFile), filepath.Join(tmpDir, currentCertFile))).NotTo(HaveOccurred())
			}

			err := kubelet.Uninstall(kubelet.UninstallOptions{
				InstallRoot: tmpDir,
				Logger:      zap.NewNop(),
			})

			// Restore permissions to allow cleanup
			if tt.makeReadOnly != "" {
				readOnlyPath := filepath.Join(tmpDir, tt.makeReadOnly)
				err := os.Chmod(filepath.Dir(readOnlyPath), 0o755)
				g.Expect(err).NotTo(HaveOccurred())
			}

			if tt.wantErr != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.wantErr))

				// For partial failure case, verify the read-only file still exists
				if tt.makeReadOnly != "" {
					readOnlyPath := filepath.Join(tmpDir, tt.makeReadOnly)
					g.Expect(readOnlyPath).To(BeAnExistingFile())
					g.Expect(os.RemoveAll(readOnlyPath)).NotTo(HaveOccurred())
				}
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}

			for _, file := range setupFiles {
				fullPath := filepath.Join(tmpDir, file)
				g.Expect(fullPath).NotTo(BeAnExistingFile())
			}
			g.Expect(filepath.Join(tmpDir, currentCertFile)).NotTo(BeAnExistingFile())
		})
	}
}

func TestInstall(t *testing.T) {
	kubectlData := []byte("test kubectl binary")

	test.RunInstallTest(t, test.TestData{
		ArtifactName: "kubelet",
		BinaryName:   "kubelet",
		Data:         kubectlData,
		Install: func(ctx context.Context, tempDir string, source aws.Source, tr *tracker.Tracker) error {
			return kubelet.Install(ctx, kubelet.InstallOptions{
				InstallRoot: tempDir,
				Tracker:     tr,
				Source:      source,
				Logger:      zap.NewNop(),
			})
		},
		Verify: func(g *GomegaWithT, tempDir string, tr *tracker.Tracker) {
			g.Expect(tr.Artifacts.Kubelet).To(BeTrue())
		},
		VerifyFilePaths: []string{kubelet.BinPath, kubelet.UnitPath},
	})
}
