package cni

import (
	"context"
	"os"

	"github.com/pkg/errors"

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

func NoOp() error {
	return nil
}

func Install(ctx context.Context, tracker *tracker.Tracker, src Source) error {
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
	if err = tracker.Add(artifact.CniPlugins); err != nil {
		return err
	}

	if err := artifact.InstallTarGz(BinPath, TgzPath); err != nil {
		return errors.Wrap(err, "failed to extract and install cni-plugins")
	}
	return nil
}

func Uninstall() error {
	return os.RemoveAll(rootDir)
}
