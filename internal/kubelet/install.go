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
	// BinPath is the path to the Kubelet binary.
	BinPath = "/usr/bin/kubelet"

	// UnitPath is the path to the Kubelet systemd unit file.
	UnitPath = "/etc/systemd/system/kubelet.service"

	artifactName      = "kubelet"
	artifactFilePerms = 0o755
)

//go:embed kubelet.service
var kubeletUnitFile []byte

// Source represents a source that serves a kubelet binary.
type Source interface {
	GetKubelet(context.Context) (artifact.Source, error)
}

// Install installs kubelet at BinPath and installs a systemd unit file at UnitPath. The systemd
// unit is configured to launch the kubelet binary.
func Install(ctx context.Context, tracker *tracker.Tracker, src Source) error {
	kubelet, err := src.GetKubelet(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get kubelet source")
	}
	defer kubelet.Close()

	if err := artifact.InstallFile(BinPath, kubelet, artifactFilePerms); err != nil {
		return errors.Wrap(err, "failed to install kubelet")
	}

	if !kubelet.VerifyChecksum() {
		return errors.Errorf("kubelet checksum mismatch: %v", artifact.NewChecksumError(kubelet))
	}
	if err = tracker.Add(artifact.Kubelet); err != nil {
		return err
	}

	buf := bytes.NewBuffer(kubeletUnitFile)

	if err := artifact.InstallFile(UnitPath, buf, 0o644); err != nil {
		return errors.Errorf("failed to install kubelet systemd unit: %v", err)
	}

	return nil
}

func Uninstall() error {
	pathsToRemove := []string{
		BinPath,
		UnitPath,
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

	return artifact.Upgrade(artifactName, BinPath, kubelet, artifactFilePerms, log)
}
