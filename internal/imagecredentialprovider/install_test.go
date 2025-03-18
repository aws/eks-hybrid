package imagecredentialprovider_test

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/internal/imagecredentialprovider"
	"github.com/aws/eks-hybrid/internal/test"
	"github.com/aws/eks-hybrid/internal/tracker"
)

func TestInstall(t *testing.T) {
	imagecredentialproviderData := []byte("test ecr-credential-provider binary")

	test.RunInstallTest(t, test.TestData{
		ArtifactName: "ecr-credential-provider",
		BinaryName:   "ecr-credential-provider",
		Data:         imagecredentialproviderData,
		Install: func(ctx context.Context, tempDir string, source aws.Source, tr *tracker.Tracker) error {
			return imagecredentialprovider.Install(ctx, imagecredentialprovider.InstallOptions{
				BinPath: filepath.Join(tempDir, "bin", "ecr-credential-provider"),
				Tracker: tr,
				Source:  source,
				Logger:  zap.NewNop(),
			})
		},
		Verify: func(g *GomegaWithT, tempDir string, tr *tracker.Tracker) {
			g.Expect(tr.Artifacts.ImageCredentialProvider).To(BeTrue())
		},
		VerifyFilePaths: []string{filepath.Join("bin", "ecr-credential-provider")},
	})
}
