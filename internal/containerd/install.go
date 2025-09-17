package containerd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	internalapi "github.com/containerd/containerd/integration/cri-api/pkg/apis"
	"github.com/containerd/containerd/integration/remote"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	v1 "k8s.io/cri-api/pkg/apis/runtime/v1"

	"github.com/aws/eks-hybrid/internal/artifact"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/system"
	"github.com/aws/eks-hybrid/internal/tracker"
	"github.com/aws/eks-hybrid/internal/util"
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

		if err := os.RemoveAll(containerdConfigDir); err != nil {
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

// Client is a containerd runtime client wrapper
// Holds the internalapi.RuntimeService for pod/container operations
type Client struct {
	Runtime internalapi.RuntimeService
}

// NewClient creates a new Client with a real containerd runtime service
func NewClient() (*Client, error) {
	runtime, err := remote.NewRuntimeService(ContainerRuntimeEndpoint, 5*time.Second)
	if err != nil {
		return nil, err
	}
	return &Client{Runtime: runtime}, nil
}

// RemovePods stops and removes all pod sandboxes and containers in the k8s.io namespace on the node
func (c *Client) RemovePods() error {
	podSandboxes, err := c.Runtime.ListPodSandbox(&v1.PodSandboxFilter{
		State: &v1.PodSandboxStateValue{
			State: v1.PodSandboxState_SANDBOX_READY,
		},
	})
	if err != nil {
		return errors.Wrap(err, "listing pod sandboxes")
	}

	for _, sandbox := range podSandboxes {
		zap.L().Info("Stopping pod..", zap.String("pod", sandbox.Metadata.Name))
		err := util.RetryExponentialBackoff(3, 2*time.Second, func() error {
			if err := c.Runtime.StopPodSandbox(sandbox.Id); err != nil {
				return errors.Wrapf(err, "stopping pod %s", sandbox.Id)
			}
			if err := c.Runtime.RemovePodSandbox(sandbox.Id); err != nil {
				return errors.Wrapf(err, "removing pod %s", sandbox.Id)
			}
			return nil
		})
		if err != nil {
			zap.L().Info("ignored error stopping pod", zap.Error(err))
		}
	}

	// If pod sandbox deletion fails, we can try to stop and remove containers individually
	// We do not pass in a container state filter here as we want to remove all containers
	// including stopped ones as they arent GCed by containerd post daemon stop.
	containers, err := c.Runtime.ListContainers(nil)
	if err != nil {
		return errors.Wrap(err, "listing containers")
	}

	for _, container := range containers {
		status, err := c.Runtime.ContainerStatus(container.Id)
		if err != nil {
			return errors.Wrapf(err, "getting container status for %s", container.Id)
		}
		zap.L().Info("Stopping container..", zap.String("container", container.Metadata.Name))
		err = util.RetryExponentialBackoff(3, 2*time.Second, func() error {
			if status.State == v1.ContainerState_CONTAINER_RUNNING {
				if err := c.Runtime.StopContainer(container.Id, 0); err != nil {
					return errors.Wrapf(err, "stopping container %s", container.Id)
				}
			}

			if err := c.Runtime.RemoveContainer(container.Id); err != nil {
				return errors.Wrapf(err, "removing container %s", container.Id)
			}
			return nil
		})
		if err != nil {
			zap.L().Info("ignored error removing container", zap.Error(err))
		}
	}
	return nil
}
