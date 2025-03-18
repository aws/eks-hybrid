package kubelet

import (
	"bytes"
	"context"
	_ "embed"
	"os"
	"path"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/tracker"
)

const (
	// DefaultBinPath is the path to the Kubelet binary.
	DefaultBinPath = "/usr/bin/kubelet"

	// DefaultUnitPath is the path to the Kubelet systemd unit file.
	DefaultUnitPath = "/etc/systemd/system/kubelet.service"

	artifactName      = "kubelet"
	artifactFilePerms = 0o755
)

//go:embed kubelet.service
var kubeletUnitFile []byte

// Source represents a source that serves a kubelet binary.
type Source interface {
	GetKubelet(context.Context) (artifact.Source, error)
}

type InstallOptions struct {
	BinPath  string
	Tracker  *tracker.Tracker
	Source   Source
	Logger   *zap.Logger
	UnitPath string
}

// Install installs kubelet at BinPath and installs a systemd unit file at UnitPath. The systemd
// unit is configured to launch the kubelet binary.
func Install(ctx context.Context, opts InstallOptions) error {
	if opts.BinPath == "" {
		opts.BinPath = DefaultBinPath
	}
	if opts.UnitPath == "" {
		opts.UnitPath = DefaultUnitPath
	}
	if err := installFromSource(ctx, opts); err != nil {
		return errors.Wrap(err, "installing kubelet")
	}

	if err := installSystemdUnit(opts.UnitPath); err != nil {
		return errors.Wrap(err, "installing systemd unit")
	}

	if err := opts.Tracker.Add(artifact.Kubelet); err != nil {
		return errors.Wrap(err, "adding kubelet to tracker")
	}

	return nil
}

func installFromSource(ctx context.Context, opts InstallOptions) error {
	// Retry up to 3 times to download and validate the checksum
	var err error
	for range 3 {
		err = downloadFileTo(ctx, opts)
		if err == nil {
			break
		}
		opts.Logger.Error("Downloading kubelet failed. Retrying...", zap.Error(err))
	}
	return err
}

func downloadFileTo(ctx context.Context, opts InstallOptions) error {
	kubelet, err := opts.Source.GetKubelet(ctx)
	if err != nil {
		return errors.Wrap(err, "getting kubelet source")
	}
	defer kubelet.Close()

	if err := artifact.InstallFile(opts.BinPath, kubelet, artifactFilePerms); err != nil {
		return errors.Wrap(err, "installing kubelet")
	}

	if !kubelet.VerifyChecksum() {
		return errors.Errorf("kubelet checksum mismatch: %v", artifact.NewChecksumError(kubelet))
	}

	return nil
}

func installSystemdUnit(unitPath string) error {
	buf := bytes.NewBuffer(kubeletUnitFile)
	if err := artifact.InstallFile(unitPath, buf, 0o644); err != nil {
		return errors.Errorf("failed to install kubelet systemd unit: %v", err)
	}
	return nil
}

func Uninstall() error {
	pathsToRemove := []string{
		DefaultBinPath,
		DefaultUnitPath,
		kubeconfigPath,
		path.Dir(kubeletConfigRoot),
	}

	for _, path := range pathsToRemove {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}

func Upgrade(ctx context.Context, src Source, log *zap.Logger) error {
	kubelet, err := src.GetKubelet(ctx)
	if err != nil {
		return errors.Wrap(err, "getting kubelet source")
	}
	defer kubelet.Close()

	return artifact.Upgrade(artifactName, DefaultBinPath, kubelet, artifactFilePerms, log)
}
