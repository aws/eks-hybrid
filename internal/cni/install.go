package cni

import (
	"context"
	"os"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/tracker"
)

const (
	rootDir = "/opt/cni"
	// BinPath is the path to the cni plugins binary.
	BinPath = "/opt/cni/bin"

	// TgzPath is the path to install the cni-plugins tgz file
	TgzPath = "/opt/cni/plugins/cni-plugins.tgz"
)

// Source represents a source that serves a cni plugins binary.
type Source interface {
	GetCniPlugins(context.Context) (artifact.Source, error)
}

func Install(ctx context.Context, tracker *tracker.Tracker, src Source) error {
	if err := installFromSource(ctx, src); err != nil {
		return err
	}
	if err := tracker.Add(artifact.CniPlugins); err != nil {
		return err
	}

	return nil
}

func installFromSource(ctx context.Context, src Source) error {
	cniPlugins, err := src.GetCniPlugins(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get cni-plugins source")
	}
	defer cniPlugins.Close()

	if err := artifact.InstallFile(TgzPath, cniPlugins, 0o755); err != nil {
		return errors.Wrap(err, "failed to install cni-plugins archive")
	}

	if !cniPlugins.VerifyChecksum() {
		return errors.Errorf("cni-plugins checksum mismatch: %v", artifact.NewChecksumError(cniPlugins))
	}

	if err := artifact.InstallTarGz(BinPath, TgzPath); err != nil {
		return errors.Wrap(err, "failed to extract and install cni-plugins")
	}
	return nil
}

func Uninstall() error {
	return os.RemoveAll(rootDir)
}

// Upgrade re-installs the cni-plugins available from the source
// Since cni-plugins is delivered as a tarball, its not possible to check if they are due for an upgrade
// todo: (@vignesh-goutham) check if we can publish cni-plugins independently with their checksum on our manifest
func Upgrade(ctx context.Context, src Source, log *zap.Logger) error {
	if err := installFromSource(ctx, src); err != nil {
		return errors.Wrapf(err, "failed to upgrade cni-plugins")
	}
	log.Info("Upgraded cni-plugins...")
	return nil
}
