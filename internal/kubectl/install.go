package kubectl

import (
	"context"
	"os"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/tracker"
)

const (
	// BinPath is the path to the kubectl binary.
	BinPath = "/usr/local/bin/kubectl"

	artifactName      = "kubectl"
	artifactFilePerms = 0o755
)

// Source represents a source that serves a kubectl binary.
type Source interface {
	GetKubectl(context.Context) (artifact.Source, error)
}

// Install installs kubectl at BinPath.
func Install(ctx context.Context, tracker *tracker.Tracker, src Source) error {
	kubectl, err := src.GetKubectl(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get kubectl source")
	}
	defer kubectl.Close()

	if err := artifact.InstallFile(BinPath, kubectl, artifactFilePerms); err != nil {
		return errors.Wrap(err, "failed to install kubectl")
	}

	if !kubectl.VerifyChecksum() {
		return errors.Errorf("kubectl checksum mismatch: %v", artifact.NewChecksumError(kubectl))
	}
	return tracker.Add(artifact.Kubectl)
}

func Uninstall() error {
	return os.RemoveAll(BinPath)
}

func Upgrade(ctx context.Context, src Source, log *zap.Logger) error {
	kubectl, err := src.GetKubectl(ctx)
	if err != nil {
		return errors.Wrap(err, "getting kubectl source")
	}
	defer kubectl.Close()

	return artifact.Upgrade(artifactName, BinPath, kubectl, artifactFilePerms, log)
}
