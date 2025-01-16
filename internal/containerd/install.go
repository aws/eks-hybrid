package containerd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/pkg/errors"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/system"
	"github.com/aws/eks-hybrid/internal/tracker"
)

type SourceName string

const (
	ContainerdSourceNone   SourceName = "none"
	ContainerdSourceDistro SourceName = "distro"
	ContainerdSourceDocker SourceName = "docker"

	containerdPackageName = "containerd"
	runcPackageName       = "runc"
)

// Source represents a source that serves a containerd binary.
type Source interface {
	GetContainerd() artifact.Package
}

func Install(ctx context.Context, tracker *tracker.Tracker, source Source, containerdSource SourceName) error {
	if containerdSource == ContainerdSourceNone {
		return nil
	}
	if isContainerdNotInstalled() {
		containerd := source.GetContainerd()
		// Sometimes install fails due to conflicts with other processes
		// updating packages, specially when automating at machine startup.
		// We assume errors are transient and just retry for a bit.
		if err := artifact.InstallPackageWithRetries(ctx, containerd, 5*time.Second); err != nil {
			return errors.Wrap(err, "failed to install containerd")
		}
		tracker.MarkContainerd(string(containerdSource))
	}
	return nil
}

func Uninstall(ctx context.Context, source Source) error {
	if isContainerdInstalled() {
		containerd := source.GetContainerd()
		if err := artifact.UninstallPackageWithRetries(ctx, containerd, 5*time.Second); err != nil {
			return errors.Wrap(err, "failed to uninstall containerd")
		}

		if err := os.RemoveAll(containerdConfigDir); err != nil {
			return errors.Wrap(err, "failed to uninstall containerd config files")
		}
	}
	return nil
}

func ValidateContainerdSource(source SourceName) error {
	osName := system.GetOsName()
	if source == ContainerdSourceNone {
		return nil
	} else if source == ContainerdSourceDocker {
		if osName == system.AmazonOsName {
			return fmt.Errorf("docker source for containerd is not supported on AL2023. Please provide `none` or `distro` to the --containerd-source flag")
		}
	} else if source == ContainerdSourceDistro {
		if osName == system.RhelOsName || osName == system.RockyOsName {
			return fmt.Errorf("distro source for containerd is not supported on %s based operating systems. Please provide `none` or `docker` to the --containerd-source flag", osName)
		}
	}
	return nil
}

func ValidateSystemdUnitFile() error {
	daemonManager, err := daemon.NewDaemonManager()
	if err != nil {
		return err
	}
	if err := daemonManager.DaemonReload(); err != nil {
		return err
	}
	daemonStatus, err := daemonManager.GetDaemonStatus(ContainerdDaemonName)
	if daemonStatus == daemon.DaemonStatusUnknown || err != nil {
		return fmt.Errorf("containerd daemon not found")
	}
	return nil
}

func GetContainerdSource(containerdSource string) SourceName {
	switch containerdSource {
	case string(ContainerdSourceDistro):
		return ContainerdSourceDistro
	case string(ContainerdSourceDocker):
		return ContainerdSourceDocker
	default:
		return ContainerdSourceNone
	}
}

func isContainerdInstalled() bool {
	_, containerdNotFoundErr := exec.LookPath(containerdPackageName)
	return containerdNotFoundErr == nil
}

// isContainerdNotInstalled returns true only if both containerd and runc are not installed
func isContainerdNotInstalled() bool {
	_, containerdNotFoundErr := exec.LookPath(containerdPackageName)
	_, runcNotFoundErr := exec.LookPath(runcPackageName)
	return containerdNotFoundErr != nil || runcNotFoundErr != nil
}
