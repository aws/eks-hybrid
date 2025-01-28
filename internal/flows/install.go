package flows

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/internal/cni"
	"github.com/aws/eks-hybrid/internal/containerd"
	"github.com/aws/eks-hybrid/internal/creds"
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

type Installer struct {
	AwsSource          aws.Source
	ContainerdSource   containerd.SourceName
	PackageManager     *packagemanager.DistroPackageManager
	CredentialProvider creds.CredentialProvider
	SsmRegion          string
	Tracker            *tracker.Tracker
	Logger             *zap.Logger
}

func (i *Installer) Run(ctx context.Context) error {
	var err error
	i.Tracker, err = tracker.GetCurrentState()
	if err != nil {
		return err
	}

	// temporary fix to re-configure package manager during upgrade which currently does full uninstall and re-install
	// TODO: move Configure() back to install command when upgrade flow is changed
	i.Logger.Info("Configuring package manager. This might take a while...")
	if err := i.PackageManager.Configure(ctx); err != nil {
		return err
	}

	if err := i.installDistroPackages(ctx); err != nil {
		return err
	}

	if err := i.installCredentialProcess(ctx); err != nil {
		return err
	}

	if err := i.installEksArtifacts(ctx); err != nil {
		return err
	}

	i.Logger.Info("Finishing up install...")
	return i.Tracker.Save()
}

func (i *Installer) installDistroPackages(ctx context.Context) error {
	i.Logger.Info("Installing containerd...")
	if err := containerd.Install(ctx, i.Tracker, i.PackageManager, i.ContainerdSource); err != nil {
		return err
	}

	i.Logger.Info("Installing iptables...")
	return iptables.Install(ctx, i.Tracker, i.PackageManager)
}

func (i *Installer) installCredentialProcess(ctx context.Context) error {
	switch i.CredentialProvider {
	case creds.IamRolesAnywhereCredentialProvider:
		i.Logger.Info("Installing AWS signing helper...")
		if err := iamrolesanywhere.Install(ctx, i.Tracker, i.AwsSource); err != nil {
			return err
		}
	case creds.SsmCredentialProvider:
		ssmInstaller := ssm.NewSSMInstaller(i.SsmRegion, i.Logger)

		if err := ssm.Install(ctx, i.Tracker, ssmInstaller); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unable to detect hybrid auth method")
	}
	return nil
}

func (i *Installer) installEksArtifacts(ctx context.Context) error {
	i.Logger.Info("Installing kubelet...")
	if err := kubelet.Install(ctx, i.Tracker, i.AwsSource); err != nil {
		return err
	}

	i.Logger.Info("Installing kubectl...")
	if err := kubectl.Install(ctx, i.Tracker, i.AwsSource); err != nil {
		return err
	}

	i.Logger.Info("Installing cni-plugins...")
	if err := cni.Install(ctx, i.Tracker, i.AwsSource); err != nil {
		return err
	}

	i.Logger.Info("Installing image credential provider...")
	if err := imagecredentialprovider.Install(ctx, i.Tracker, i.AwsSource); err != nil {
		return err
	}

	i.Logger.Info("Installing IAM authenticator...")
	return iamauthenticator.Install(ctx, i.Tracker, i.AwsSource)
}
