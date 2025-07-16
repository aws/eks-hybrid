package containerd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

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

func Uninstall(ctx context.Context, source Source, logger *zap.Logger) error {
	logger.Info("Starting containerd uninstall process")

	if isContainerdInstalled() {
		logger.Info("Containerd is installed, proceeding with uninstall")

		containerd := source.GetContainerd(ContainerdVersion)
		logger.Info("Executing containerd uninstall command")
		if err := cmd.Retry(ctx, containerd.UninstallCmd, 5*time.Second); err != nil {
			logger.Error("Failed to uninstall containerd package", zap.Error(err))
			return errors.Wrap(err, "uninstalling containerd")
		}
		logger.Info("Successfully uninstalled containerd package")

		passwdFile := "/etc/passwd"
		if _, err := os.Stat(passwdFile); os.IsNotExist(err) {
			logger.Warn("Before /etc/passwd file does not exist", zap.String("path", passwdFile))
		} else if err != nil {
			logger.Error("Before Error checking /etc/passwd file status", zap.String("path", passwdFile), zap.Error(err))
		} else {
			logger.Info("Before /etc/passwd file is present", zap.String("path", passwdFile))
		}

		logger.Info("Removing containerd config directory", zap.String("path", containerdConfigDir))
		if err := os.RemoveAll(containerdConfigDir); err != nil {
			logger.Error("Failed to remove containerd config directory", zap.String("path", containerdConfigDir), zap.Error(err))
			return errors.Wrap(err, "removing containerd config files")
		}
		logger.Info("Successfully removed containerd config directory", zap.String("path", containerdConfigDir))

		if _, err := os.Stat(passwdFile); os.IsNotExist(err) {
			logger.Warn("After /etc/passwd file does not exist", zap.String("path", passwdFile))
		} else if err != nil {
			logger.Error("After Error checking /etc/passwd file status", zap.String("path", passwdFile), zap.Error(err))
		} else {
			logger.Info("After /etc/passwd file is present", zap.String("path", passwdFile))
		}
	} else {
		logger.Info("Containerd is not installed, skipping uninstall")
	}

	logger.Info("Containerd uninstall process completed successfully")
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
