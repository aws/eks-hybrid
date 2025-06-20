package flows

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	awsSsm "github.com/aws/aws-sdk-go-v2/service/ssm"
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
	CNIUninstall func() error
)

type Uninstaller struct {
	Artifacts      *tracker.InstalledArtifacts
	DaemonManager  daemon.DaemonManager
	PackageManager *packagemanager.DistroPackageManager
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
		if err := kubelet.Uninstall(kubelet.UninstallOptions{}); err != nil {
			return err
		}
	}
	if u.Artifacts.Ssm {
		u.Logger.Info("Stopping SSM daemon...")
		if err := u.DaemonManager.StopDaemon(ssm.SsmDaemonName); err != nil {
			return err
		}

		ssmRegistration := ssm.NewSSMRegistration()
		region := ssmRegistration.GetRegion()
		opts := []func(*config.LoadOptions) error{}
		if region != "" {
			opts = append(opts, config.WithRegion(region))
		}

		awsConfig, err := config.LoadDefaultConfig(ctx, opts...)
		if err != nil {
			return err
		}

		ssmClient := awsSsm.NewFromConfig(awsConfig, func(o *awsSsm.Options) {
			// intentionally long max backoff and number of retry attempts as we want to optimize for success
			// vs flaky fails during deregistering due to connection reset (and the like) errors from the ssm endpoint
			// we would rather longer run time than flaky failures
			o.Retryer = retry.AddWithMaxAttempts(o.Retryer, 12)
			o.Retryer = retry.AddWithMaxBackoffDelay(o.Retryer, 1*time.Minute)
		})
		if err := ssm.Uninstall(ctx, ssm.UninstallOptions{
			Logger:          u.Logger,
			SSMRegistration: ssmRegistration,
			PkgSource:       u.PackageManager,
			SSMClient:       ssmClient,
		}); err != nil {
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
	if u.Artifacts.Containerd != tracker.ContainerdSourceNone {
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
