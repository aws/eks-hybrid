package kubectl_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/internal/kubectl"
	"github.com/aws/eks-hybrid/internal/test"
	"github.com/aws/eks-hybrid/internal/tracker"
)

func TestInstall(t *testing.T) {
	kubectlData := []byte("test kubectl binary")

	test.RunInstallTest(t, test.TestData{
		ArtifactName: "kubectl",
		BinaryName:   "kubectl",
		Data:         kubectlData,
		Install: func(ctx context.Context, tempDir string, source aws.Source, tr *tracker.Tracker) error {
			return kubectl.Install(ctx, kubectl.InstallOptions{
				BinPath: filepath.Join(tempDir, "bin", "kubectl"),
				Tracker: tr,
				Source:  source,
				Logger:  zap.NewNop(),
			})
		},
		Verify: func(g *GomegaWithT, tempDir string, tr *tracker.Tracker) {
			g.Expect(tr.Artifacts.Kubectl).To(BeTrue())

			kubectlPath := filepath.Join(tempDir, "bin", "kubectl")
			_, err := os.Stat(kubectlPath)
			g.Expect(err).NotTo(HaveOccurred())
		},
	})
}
