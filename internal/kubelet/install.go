package kubelet

import (
	"bytes"
	"context"
	_ "embed"
	stdErrors "errors"
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/system"
	"github.com/aws/eks-hybrid/internal/tracker"
)

const (
	// DefaultBinPath is the path to the Kubelet binary.
	BinPath = "/usr/bin/kubelet"

	// UnitPath is the path to the Kubelet systemd unit file.
	UnitPath = "/etc/systemd/system/kubelet.service"

	artifactName      = "kubelet"
	artifactFilePerms = 0o755
)

var KubeletCurrentCertPath = path.Join(kubeconfigRoot, "pki", "kubelet-server-current.pem")

//go:embed kubelet.service
var kubeletUnitFile []byte

// Source represents a source that serves a kubelet binary.
type Source interface {
	GetKubelet(context.Context) (artifact.Source, error)
}

type InstallOptions struct {
	InstallRoot string
	Tracker     *tracker.Tracker
	Source      Source
	Logger      *zap.Logger
}

// Install installs kubelet at BinPath and installs a systemd unit file at UnitPath. The systemd
// unit is configured to launch the kubelet binary.
func Install(ctx context.Context, opts InstallOptions) error {
	if err := installFromSource(ctx, opts); err != nil {
		return errors.Wrap(err, "installing kubelet")
	}

	if err := installSystemdUnit(filepath.Join(opts.InstallRoot, UnitPath)); err != nil {
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

	if err := artifact.InstallFile(filepath.Join(opts.InstallRoot, BinPath), kubelet, artifactFilePerms); err != nil {
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

type UninstallOptions struct {
	// InstallRoot is optionally the root directory of the installation
	// If not provided, the default will be /
	InstallRoot string
}

func Uninstall(opts UninstallOptions) error {
	pathsToRemove := []string{
		filepath.Join(opts.InstallRoot, BinPath),
		filepath.Join(opts.InstallRoot, UnitPath),
		filepath.Join(opts.InstallRoot, kubeconfigPath),
		filepath.Join(opts.InstallRoot, path.Dir(kubeletConfigRoot)),
		filepath.Join(opts.InstallRoot, KubeletCurrentCertPath),
	}

	allErrors := []error{}

	// resolve the symlink and add actual file to remove
	actualCertPath, err := filepath.EvalSymlinks(filepath.Join(opts.InstallRoot, KubeletCurrentCertPath))
	if err != nil && !os.IsNotExist(err) {
		allErrors = append(allErrors, errors.Wrap(err, "resolving symlink for kubelet cert"))
	}
	if actualCertPath != "" {
		pathsToRemove = append(pathsToRemove, actualCertPath)
	}

	for _, path := range pathsToRemove {
		if err := system.SafeRemoveAll(path, false, false); err != nil {
			allErrors = append(allErrors, err)
		}
	}
	if len(allErrors) > 0 {
		return stdErrors.Join(allErrors...)
	}
	return nil
}

func Upgrade(ctx context.Context, src Source, log *zap.Logger) error {
	kubelet, err := src.GetKubelet(ctx)
	if err != nil {
		return errors.Wrap(err, "getting kubelet source")
	}
	defer kubelet.Close()

	return artifact.Upgrade(artifactName, BinPath, kubelet, artifactFilePerms, log)
}
