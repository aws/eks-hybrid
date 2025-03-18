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
	// DefaultBinPath is the path to the cni plugins binary.
	DefaultBinPath = "/opt/cni/bin"

	// TgzPath is the path to install the cni-plugins tgz file
	DefaultTgzPath = "/opt/cni/plugins/cni-plugins.tgz"

	artifactName = "cni-plugins"
)

// Source represents a source that serves a cni plugins binary.
type Source interface {
	GetCniPlugins(context.Context) (artifact.Source, error)
}

type InstallOptions struct {
	BinPath string
	Logger  *zap.Logger
	Source  Source
	TgzPath string
	Tracker *tracker.Tracker
}

func Install(ctx context.Context, opts InstallOptions) error {
	if err := installFromSource(ctx, opts); err != nil {
		return err
	}

	if err := opts.Tracker.Add(artifact.CniPlugins); err != nil {
		return errors.Wrap(err, "adding cni-plugins to tracker")
	}

	return nil
}

func installFromSource(ctx context.Context, opts InstallOptions) error {
	if opts.BinPath == "" {
		opts.BinPath = DefaultBinPath
	}
	if opts.TgzPath == "" {
		opts.TgzPath = DefaultTgzPath
	}
	if err := downloadFileWithRetries(ctx, opts); err != nil {
		return errors.Wrap(err, "installing cni-plugins")
	}

	if err := artifact.InstallTarGz(opts.BinPath, opts.TgzPath); err != nil {
		return errors.Wrap(err, "extracting and installing cni-plugins")
	}

	return nil
}

func downloadFileWithRetries(ctx context.Context, opts InstallOptions) error {
	// Retry up to 3 times to download and validate the checksum
	var err error
	for range 3 {
		err = downloadFileTo(ctx, opts)
		if err == nil {
			break
		}
		opts.Logger.Error("Downloading cni-plugins failed. Retrying...", zap.Error(err))
	}
	return err
}

func downloadFileTo(ctx context.Context, opts InstallOptions) error {
	cniPlugins, err := opts.Source.GetCniPlugins(ctx)
	if err != nil {
		return errors.Wrap(err, "getting cni-plugins source")
	}
	defer cniPlugins.Close()

	if err := artifact.InstallFile(opts.TgzPath, cniPlugins, 0o755); err != nil {
		return errors.Wrap(err, "installing cni-plugins archive")
	}

	if !cniPlugins.VerifyChecksum() {
		return errors.Errorf("cni-plugins checksum mismatch: %v", artifact.NewChecksumError(cniPlugins))
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
	opts := InstallOptions{
		Source: src,
		Logger: log,
	}
	if err := installFromSource(ctx, opts); err != nil {
		return errors.Wrapf(err, "upgrading cni-plugins")
	}
	log.Info("Upgraded", zap.String("artifact", artifactName))
	return nil
}
