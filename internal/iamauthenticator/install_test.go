package iamauthenticator_test

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/internal/iamauthenticator"
	"github.com/aws/eks-hybrid/internal/test"
	"github.com/aws/eks-hybrid/internal/tracker"
)

func TestInstall(t *testing.T) {
	iamauthenticatorData := []byte("test aws-iam-authenticator binary")

	test.RunInstallTest(t, test.TestData{
		ArtifactName: "aws-iam-authenticator",
		BinaryName:   "aws-iam-authenticator",
		Data:         iamauthenticatorData,
		Install: func(ctx context.Context, tempDir string, source aws.Source, tr *tracker.Tracker) error {
			return iamauthenticator.Install(ctx, iamauthenticator.InstallOptions{
				BinPath: filepath.Join(tempDir, "bin", "aws-iam-authenticator"),
				Tracker: tr,
				Source:  source,
				Logger:  zap.NewNop(),
			})
		},
		Verify: func(g *GomegaWithT, tempDir string, tr *tracker.Tracker) {
			g.Expect(tr.Artifacts.IamAuthenticator).To(BeTrue())
		},
		VerifyFilePaths: []string{filepath.Join("bin", "aws-iam-authenticator")},
	})
}
