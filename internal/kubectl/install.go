package kubectl

import (
	"context"
	"os"
	"path/filepath"

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

// InstallOptions contains options for installing kubectl
type InstallOptions struct {
	InstallRoot string
	Tracker     *tracker.Tracker
	Source      Source
	Logger      *zap.Logger
}

// Install installs kubectl at BinPath.
func Install(ctx context.Context, opts InstallOptions) error {
	if err := installFromSource(ctx, opts); err != nil {
		return errors.Wrap(err, "installing kubectl")
	}

	if err := opts.Tracker.Add(artifact.Kubectl); err != nil {
		return errors.Wrap(err, "adding kubectl to tracker")
	}

	return nil
}

func installFromSource(ctx context.Context, opts InstallOptions) error {
	if err := downloadFileWithRetries(ctx, opts); err != nil {
		return errors.Wrap(err, "downloading kubectl")
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
		opts.Logger.Error("Downloading kubectl failed. Retrying...", zap.Error(err))
	}
	return err
}

func downloadFileTo(ctx context.Context, opts InstallOptions) error {
	kubectl, err := opts.Source.GetKubectl(ctx)
	if err != nil {
		return errors.Wrap(err, "getting kubectl source")
	}
	defer kubectl.Close()

	if err := artifact.InstallFile(filepath.Join(opts.InstallRoot, BinPath), kubectl, artifactFilePerms); err != nil {
		return errors.Wrap(err, "installing kubectl")
	}

	if !kubectl.VerifyChecksum() {
		return errors.Errorf("kubectl checksum mismatch: %v", artifact.NewChecksumError(kubectl))
	}

	return nil
}

func Uninstall(logger *zap.Logger) error {
	logger.Info("Uninstalling kubectl", zap.String("path", BinPath))
	if err := os.RemoveAll(BinPath); err != nil {
		logger.Error("Failed to remove kubectl binary", zap.String("path", BinPath), zap.Error(err))
		return err
	}
	logger.Info("Successfully removed kubectl binary", zap.String("path", BinPath))
	return nil
}

func Upgrade(ctx context.Context, src Source, log *zap.Logger) error {
	kubectl, err := src.GetKubectl(ctx)
	if err != nil {
		return errors.Wrap(err, "getting kubectl source")
	}
	defer kubectl.Close()

	return artifact.Upgrade(artifactName, BinPath, kubectl, artifactFilePerms, log)
}
