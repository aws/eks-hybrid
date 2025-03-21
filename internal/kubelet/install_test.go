package kubelet_test

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/test"
	"github.com/aws/eks-hybrid/internal/tracker"
)

func TestInstall(t *testing.T) {
	kubectlData := []byte("test kubectl binary")

	test.RunInstallTest(t, test.TestData{
		ArtifactName: "kubelet",
		BinaryName:   "kubelet",
		Data:         kubectlData,
		Install: func(ctx context.Context, tempDir string, source aws.Source, tr *tracker.Tracker) error {
			return kubelet.Install(ctx, kubelet.InstallOptions{
				BinPath:  filepath.Join(tempDir, "bin", "kubelet"),
				Tracker:  tr,
				Source:   source,
				Logger:   zap.NewNop(),
				UnitPath: filepath.Join(tempDir, "kubelet.service"),
			})
		},
		Verify: func(g *GomegaWithT, tempDir string, tr *tracker.Tracker) {
			g.Expect(tr.Artifacts.Kubelet).To(BeTrue())
		},
		VerifyFilePaths: []string{filepath.Join("bin", "kubelet"), filepath.Join("kubelet.service")},
	})
}
