package flows

import (
	"context"
	"fmt"
	"os"

	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/containerd"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/iamauthenticator"
	"github.com/aws/eks-hybrid/internal/iamrolesanywhere"
	"github.com/aws/eks-hybrid/internal/imagecredentialprovider"
	"github.com/aws/eks-hybrid/internal/iptables"
	"github.com/aws/eks-hybrid/internal/kubectl"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/packagemanager"
	"github.com/aws/eks-hybrid/internal/ssm"
	"github.com/aws/eks-hybrid/internal/tracker"
)

const eksConfigDir = "/etc/eks"

type (
	SSMUninstall func(ctx context.Context, logger *zap.Logger, pm ssm.PkgSource) error
	CNIUninstall func() error
)

type Uninstaller struct {
	Artifacts      *tracker.InstalledArtifacts
	DaemonManager  daemon.DaemonManager
	PackageManager *packagemanager.DistroPackageManager
	SSMUninstall   SSMUninstall
	Logger         *zap.Logger
	CNIUninstall   CNIUninstall
}

func (u *Uninstaller) Run(ctx context.Context) error {
	if err := u.uninstallDaemons(ctx); err != nil {
		return err
	}

	if err := u.uninstallBinaries(ctx); err != nil {
		return err
	}

	if err := u.cleanup(); err != nil {
		return err
	}

	u.Logger.Info("Finished uninstallation tasks...")

	return tracker.Clear()
}

func (u *Uninstaller) uninstallDaemons(ctx context.Context) error {
	if u.Artifacts.Kubelet {
		u.Logger.Info("Uninstalling kubelet...")
		if err := u.DaemonManager.StopDaemon(kubelet.KubeletDaemonName); err != nil {
			return err
		}
		if err := kubelet.Uninstall(); err != nil {
			return err
		}
	}
	if u.Artifacts.Ssm {
		u.Logger.Info("Stopping SSM daemon...")
		if err := u.DaemonManager.StopDaemon(ssm.SsmDaemonName); err != nil {
			return err
		}

		if err := u.SSMUninstall(ctx, u.Logger, u.PackageManager); err != nil {
			return fmt.Errorf("uninstalling SSM: %w", err)
		}
	}
	if u.Artifacts.IamRolesAnywhere {
		u.Logger.Info("Removing aws_signing_helper_update daemon...")
		if status, err := u.DaemonManager.GetDaemonStatus(iamrolesanywhere.DaemonName); err == nil || status != daemon.DaemonStatusUnknown {
			if err = u.DaemonManager.StopDaemon(iamrolesanywhere.DaemonName); err != nil {
				u.Logger.Info("Stopping aws_signing_helper_update daemon...")
				return err
			}
		}
	}
	if u.Artifacts.Containerd != string(containerd.ContainerdSourceNone) {
		u.Logger.Info("Uninstalling containerd...")
		if err := u.DaemonManager.StopDaemon(containerd.ContainerdDaemonName); err != nil {
			return err
		}
		if err := containerd.Uninstall(ctx, u.PackageManager); err != nil {
			return err
		}
	}
	return nil
}

func (u *Uninstaller) uninstallBinaries(ctx context.Context) error {
	if u.Artifacts.Kubectl {
		u.Logger.Info("Uninstalling kubectl...")
		if err := kubectl.Uninstall(); err != nil {
			return err
		}
	}
	if u.Artifacts.CniPlugins {
		u.Logger.Info("Uninstalling cni-plugins...")
		if err := u.CNIUninstall(); err != nil {
			return err
		}
	}
	if u.Artifacts.IamAuthenticator {
		u.Logger.Info("Uninstalling IAM authenticator...")
		if err := iamauthenticator.Uninstall(); err != nil {
			return err
		}
	}
	if u.Artifacts.IamRolesAnywhere {
		u.Logger.Info("Uninstalling AWS signing helper...")
		if err := iamrolesanywhere.Uninstall(); err != nil {
			return err
		}
	}
	if u.Artifacts.ImageCredentialProvider {
		u.Logger.Info("Uninstalling image credential provider...")
		if err := imagecredentialprovider.Uninstall(); err != nil {
			return err
		}
	}
	if u.Artifacts.Iptables {
		u.Logger.Info("Uninstalling iptables...")
		if err := iptables.Uninstall(ctx, u.PackageManager); err != nil {
			return err
		}
	}
	return nil
}

// cleanup removes directories or files that are not individually owned by single component
func (u *Uninstaller) cleanup() error {
	if err := u.PackageManager.Cleanup(); err != nil {
		return err
	}

	if err := os.RemoveAll(eksConfigDir); err != nil {
		return err
	}

	return nil
}
