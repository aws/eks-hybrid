package containerd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/mod/semver"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/system"
	"github.com/aws/eks-hybrid/internal/tracker"
	"github.com/aws/eks-hybrid/internal/util/cmd"
)

const (
	containerdPackageName = "containerd"
	runcPackageName       = "runc"
)

// Source represents a source that serves a containerd binary.
type Source interface {
	GetContainerd(version string) artifact.Package
}

func Install(ctx context.Context, artifactsTracker *tracker.Tracker, source Source, containerdSource tracker.ContainerdSourceName, kubernetesVersion, containerdVersion string) error {
	if err := ValidateContainerdVersion(containerdVersion, kubernetesVersion, containerdSource); err != nil {
		return err
	}
	// An explicit package version must be installed even if containerd and runc already exist.
	// Otherwise, if containerd/runc are already installed, we skip the installation and set the source to none
	// which exclude it from being upgrading during upgrade and removed during uninstall
	// this has the (potentially negative) side effect of the user not knowing that we have chosen none on
	// their behalf based on it already being installed
	// TODO: a better approach would be to determine if the installed versions are from the user supplied
	// containerd-source (distro/docker) and if they are, treat it as such including upgrading/uninstalling
	// if they are not, we error and ask the user to explictly pass none to the --containerd-source flag
	if containerdSource == tracker.ContainerdSourceNone || (containerdVersion == "" && areContainerdAndRuncInstalled()) {
		artifactsTracker.Artifacts.Containerd = tracker.ContainerdSourceNone
		return nil
	}
	containerdVersionConstraint := determineContainerdVersionConstraint(kubernetesVersion)
	if containerdVersion != "" {
		containerdVersionConstraint = containerdVersion
	}
	containerd := source.GetContainerd(containerdVersionConstraint)
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
		// Use no version constraint to uninstall any version (1.x or 2.x)
		containerd := source.GetContainerd("")
		if err := cmd.Retry(ctx, containerd.UninstallCmd, 5*time.Second); err != nil {
			return errors.Wrap(err, "uninstalling containerd")
		}

		if err := os.RemoveAll(containerdConfigDir); err != nil {
			return errors.Wrap(err, "removing containerd config files")
		}
	}
	return nil
}

func Upgrade(ctx context.Context, source Source, kubernetesVersion string, skipContainerdMajorVersionUpgrade bool) error {
	var containerdVersionConstraint string

	if skipContainerdMajorVersionUpgrade {
		containerdMajorVersion, err := GetContainerdMajorVersion()
		if err != nil {
			return err
		}

		// pins containerd upgrade version to current installed major version
		switch containerdMajorVersion {
		case 1:
			containerdVersionConstraint = "1.*"
		case 2:
			containerdVersionConstraint = "2.0.*"
		default:
			return fmt.Errorf("unsupported containerd major version: %d", containerdMajorVersion)
		}
	} else {
		// Upgrade containerd to latest compatible version including major version upgrade (1.x -> 2.x) if available
		containerdVersionConstraint = determineContainerdVersionConstraint(kubernetesVersion)
	}

	containerd := source.GetContainerd(containerdVersionConstraint)
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

func determineContainerdVersionConstraint(kubernetesVersion string) string {
	containerdVersionConstraint := "1.*"
	// Enable newest containerd version available in package manager (including 2.x) for K8s versions >= 1.30
	if semver.Compare("v"+kubernetesVersion, "v1.30.0") >= 0 {
		containerdVersionConstraint = ""
	}
	return containerdVersionConstraint
}

// containerdPackageVersion accepts exact package versions, including Debian epochs
// and distribution release suffixes, but excludes package-manager wildcards.
var containerdPackageVersion = regexp.MustCompile(`^(?:[0-9]+:)?([0-9]+\.[0-9]+\.[0-9]+)(?:[-+~.][0-9A-Za-z.+~_-]+)?$`)

// ValidateContainerdVersion preserves the compatibility policy used for automatic
// version selection while accepting native package version strings.
func ValidateContainerdVersion(version, kubernetesVersion string, source tracker.ContainerdSourceName) error {
	if version == "" {
		return nil
	}
	if source == tracker.ContainerdSourceNone {
		return fmt.Errorf("--containerd-version cannot be used with --containerd-source none")
	}
	matches := containerdPackageVersion.FindStringSubmatch(version)
	if matches == nil || !semver.IsValid("v"+matches[1]) {
		return fmt.Errorf("invalid containerd package version %q: specify an exact major.minor.patch version with an optional epoch or release suffix", version)
	}
	if kubernetesVersion != "" && determineContainerdVersionConstraint(kubernetesVersion) == "1.*" && semver.Major("v"+matches[1]) != "v1" {
		return fmt.Errorf("containerd package version %q is incompatible with Kubernetes %s: Kubernetes versions before 1.30 require containerd 1.x", version, kubernetesVersion)
	}
	return nil
}
