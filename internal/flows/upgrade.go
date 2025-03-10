package flows

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/internal/cni"
	"github.com/aws/eks-hybrid/internal/containerd"
	"github.com/aws/eks-hybrid/internal/creds"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/iamauthenticator"
	"github.com/aws/eks-hybrid/internal/iamrolesanywhere"
	"github.com/aws/eks-hybrid/internal/imagecredentialprovider"
	"github.com/aws/eks-hybrid/internal/iptables"
	"github.com/aws/eks-hybrid/internal/kubectl"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/nodeprovider"
	"github.com/aws/eks-hybrid/internal/packagemanager"
	"github.com/aws/eks-hybrid/internal/ssm"
	"github.com/aws/eks-hybrid/internal/tracker"
)

type Upgrader struct {
	NodeProvider       nodeprovider.NodeProvider
	AwsSource          aws.Source
	PackageManager     *packagemanager.DistroPackageManager
	CredentialProvider creds.CredentialProvider
	Artifacts          *tracker.InstalledArtifacts
	DaemonManager      daemon.DaemonManager
	SkipPhases         []string
	Logger             *zap.Logger
}

func (u *Upgrader) Run(ctx context.Context) error {
	if err := u.upgradeDistroPackages(ctx); err != nil {
		return err
	}

	if err := u.upgradeCredentialProvider(ctx); err != nil {
		return err
	}

	if err := u.upgradeEksArtifacts(ctx); err != nil {
		return err
	}

	if err := u.NodeProvider.ConfigureAws(ctx); err != nil {
		return err
	}
	if err := u.NodeProvider.Enrich(ctx); err != nil {
		return err
	}
	if err := initDaemons(ctx, u.NodeProvider, u.SkipPhases, u.Logger); err != nil {
		return err
	}

	return u.NodeProvider.Cleanup()
}

func (u *Upgrader) upgradeDistroPackages(ctx context.Context) error {
	u.Logger.Info("Refreshing package manager metadata cache...")
	if err := u.PackageManager.RefreshMetadataCache(ctx); err != nil {
		return err
	}
	if u.Artifacts.Containerd != string(containerd.ContainerdSourceNone) {
		u.Logger.Info("Upgrading containerd...")
		if err := containerd.Upgrade(ctx, u.PackageManager); err != nil {
			return err
		}
	}

	if u.Artifacts.Iptables {
		u.Logger.Info("Upgrading iptables...")
		if err := iptables.Upgrade(ctx, u.PackageManager); err != nil {
			return err
		}
	}
	return nil
}

func (u *Upgrader) upgradeCredentialProvider(ctx context.Context) error {
	switch u.CredentialProvider {
	case creds.IamRolesAnywhereCredentialProvider:
		u.Logger.Info("Upgrading AWS signing helper...")
		if err := iamrolesanywhere.Upgrade(ctx, u.AwsSource, u.Logger); err != nil {
			return err
		}
	case creds.SsmCredentialProvider:
		nodeConfig := u.NodeProvider.GetNodeConfig()
		ssmInstaller := ssm.NewSSMInstaller(u.Logger, nodeConfig.Spec.Cluster.Region)

		u.Logger.Info("Upgrading SSM agent installer...")
		if err := ssm.Upgrade(ctx, ssm.InstallOptions{
			Source: ssmInstaller,
			Logger: u.Logger,
			Region: nodeConfig.Spec.Cluster.Region,
		}); err != nil {
			return err
		}
	default:
		return fmt.Errorf("installed credential provider %s is not supported for upgrade", u.CredentialProvider)
	}
	return nil
}

func (u *Upgrader) upgradeEksArtifacts(ctx context.Context) error {
	u.Logger.Info("Upgrading kubelet...")
	if err := kubelet.Upgrade(ctx, u.AwsSource, u.Logger); err != nil {
		return errors.Wrap(err, "failed to upgrade kubelet")
	}

	u.Logger.Info("Upgrading kubectl...")
	if err := kubectl.Upgrade(ctx, u.AwsSource, u.Logger); err != nil {
		return err
	}

	u.Logger.Info("Upgrading image credential provider...")
	if err := imagecredentialprovider.Upgrade(ctx, u.AwsSource, u.Logger); err != nil {
		return err
	}

	u.Logger.Info("Upgrading IAM authenticator...")
	if err := iamauthenticator.Upgrade(ctx, u.AwsSource, u.Logger); err != nil {
		return err
	}

	u.Logger.Info("Upgrading cni-plugins...")
	return cni.Upgrade(ctx, u.AwsSource, u.Logger)
}
