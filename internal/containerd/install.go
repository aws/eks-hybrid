package containerd

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/pkg/errors"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/system"
	"github.com/aws/eks-hybrid/internal/tracker"
	"github.com/aws/eks-hybrid/internal/util/cmd"
)

const (
	// pin containerd to major version 1.x
	ContainerdVersion = "1.*"

	containerdPackageName = "containerd"
	runcPackageName       = "runc"
)

// Source represents a source that serves a containerd binary.
type Source interface {
	GetContainerd(version string) artifact.Package
}

func Install(ctx context.Context, artifactsTracker *tracker.Tracker, source Source, containerdSource tracker.ContainerdSourceName) error {
	// if containerd/run are already installed, we skip the installation and set the source to none
	// which exclude it from being upgrading during upgrade and removed during uninstall
	// this has the (potentially negative) side effect of the user not knowing that we have chosen none on
	// their behalf based on it already being installed
	// TODO: a better approach would be to determine if the installed versions are from the user supplied
	// containerd-source (distro/docker) and if they are, treat it as such including upgrading/uninstalling
	// if they are not, we error and ask the user to explictly pass none to the --containerd-source flag
	if containerdSource == tracker.ContainerdSourceNone || areContainerdAndRuncInstalled() {
		artifactsTracker.Artifacts.Containerd = tracker.ContainerdSourceNone
		return nil
	}
	containerd := source.GetContainerd(ContainerdVersion)
	// Sometimes install fails due to conflicts with other processes
	// updating packages, specially when automating at machine startup.
	// We assume errors are transient and just retry for a bit.
	if err := cmd.Retry(ctx, containerd.InstallCmd, 5*time.Second); err != nil {
		return errors.Wrap(err, "installing containerd")
	}
	artifactsTracker.Artifacts.Containerd = containerdSource
	return nil
}

func Uninstall(ctx context.Context, source Source) error {
	if isContainerdInstalled() {
		containerd := source.GetContainerd(ContainerdVersion)
		if err := cmd.Retry(ctx, containerd.UninstallCmd, 5*time.Second); err != nil {
			return errors.Wrap(err, "uninstalling containerd")
		}

		if err := system.SafeRemoveAll(containerdConfigDir, false, false); err != nil {
			return errors.Wrap(err, "removing containerd config files")
		}
	}
	return nil
}

func Upgrade(ctx context.Context, source Source) error {
	containerd := source.GetContainerd(ContainerdVersion)
	if err := cmd.Retry(ctx, containerd.UpgradeCmd, 5*time.Second); err != nil {
		return errors.Wrap(err, "upgrading containerd")
	}
	return nil
}

func ValidateContainerdSource(source tracker.ContainerdSourceName) error {
	osName := system.GetOsName()
	switch source {
	case tracker.ContainerdSourceNone:
		return nil
	case tracker.ContainerdSourceDocker:
		if osName == system.AmazonOsName {
			return fmt.Errorf("docker source for containerd is not supported on AL2023. Please provide `none` or `distro` to the --containerd-source flag")
		}
	case tracker.ContainerdSourceDistro:
		if osName == system.RhelOsName {
			return fmt.Errorf("distro source for containerd is not supported on RHEL. Please provide `none` or `docker` to the --containerd-source flag")
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

func isContainerdInstalled() bool {
	_, containerdNotFoundErr := exec.LookPath(containerdPackageName)
	return containerdNotFoundErr == nil
}

// areContainerdAndRuncInstalled returns true only if both containerd and runc are installed
func areContainerdAndRuncInstalled() bool {
	_, containerdNotFoundErr := exec.LookPath(containerdPackageName)
	_, runcNotFoundErr := exec.LookPath(runcPackageName)
	return containerdNotFoundErr == nil && runcNotFoundErr == nil
}
