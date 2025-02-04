package kubectl

import (
	"context"
	"os"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/tracker"
)

// BinPath is the path to the kubectl binary.
const BinPath = "/usr/local/bin/kubectl"

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

	if err := artifact.InstallFile(BinPath, kubectl, 0o755); err != nil {
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
		return errors.Wrap(err, "failed to get kubectl source")
	}
	defer kubectl.Close()

	upgradable, err := artifact.UpgradeAvailable(BinPath, kubectl)
	if err != nil {
		return err
	}

	if upgradable {
		if err := artifact.InstallFile(BinPath, kubectl, 0o755); err != nil {
			return errors.Wrap(err, "failed to upgrade kubectl")
		}

		if !kubectl.VerifyChecksum() {
			return errors.Errorf("kubectl checksum mismatch: %v", artifact.NewChecksumError(kubectl))
		}

		log.Info("Upgraded kubectl...")
	} else {
		log.Info("No new version of kubectl found. Skipping upgrade...")
	}
	return nil
}
