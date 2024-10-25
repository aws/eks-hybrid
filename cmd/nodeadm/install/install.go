package install

import (
	"context"
	"fmt"
	"time"

	"github.com/integrii/flaggy"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/internal/cli"
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

func NewCommand() cli.Command {
	cmd := command{
		downloadTimeout: 5 * time.Minute,
	}

	fc := flaggy.NewSubcommand("install")
	fc.Description = "Install components required to join an EKS cluster"
	fc.AddPositionalValue(&cmd.kubernetesVersion, "KUBERNETES_VERSION", 1, true, "The major[.minor[.patch]] version of Kubernetes to install")
	fc.String(&cmd.credentialProvider, "p", "credential-provider", "Credential process to install. Allowed values are ssm & iam-ra")
	fc.String(&cmd.containerdSource, "cs", "containerd-source", "Source for containerd artifact. Allowed values are none, distro & docker")
	fc.Duration(&cmd.downloadTimeout, "dt", "download-timeout", "Timeout for downloading artifacts. Input follows duration format. Example: 1h23s")
	cmd.flaggy = fc

	return &cmd
}

type command struct {
	flaggy             *flaggy.Subcommand
	kubernetesVersion  string
	credentialProvider string
	containerdSource   string
	downloadTimeout    time.Duration
}

type Config struct {
	AwsSource          aws.Source
	ContainerdSource   containerd.SourceName
	CredentialProvider creds.CredentialProvider
	Log                *zap.Logger
	DownloadTimeout    time.Duration
}

func (c *command) Flaggy() *flaggy.Subcommand {
	return c.flaggy
}

func (c *command) Run(log *zap.Logger, opts *cli.GlobalOptions) error {
	root, err := cli.IsRunningAsRoot()
	if err != nil {
		return err
	}
	if !root {
		return cli.ErrMustRunAsRoot
	}
	credentialProvider, err := creds.GetCredentialProvider(c.credentialProvider)
	if err != nil {
		return err
	}

	// Default containerd source to distro
	if c.containerdSource == "" {
		c.containerdSource = string(containerd.ContainerdSourceDistro)
	}
	containerdSource := containerd.GetContainerdSource(c.containerdSource)
	if err := containerd.ValidateContainerdSource(containerdSource); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.downloadTimeout)
	defer cancel()

	log.Info("Validating Kubernetes version", zap.Reflect("kubernetes version", c.kubernetesVersion))
	// Create a Source for all AWS managed artifacts.
	awsSource, err := aws.GetLatestSource(ctx, c.kubernetesVersion)
	if err != nil {
		return err
	}
	log.Info("Using Kubernetes version", zap.Reflect("kubernetes version", awsSource.Eks.Version))

	config := &Config{
		AwsSource:          awsSource,
		ContainerdSource:   containerdSource,
		CredentialProvider: credentialProvider,
		DownloadTimeout:    c.downloadTimeout,
		Log:                log,
	}

	return config.Install(ctx)
}

func (c *Config) Install(ctx context.Context) error {
	// Create tracker with existing changes or new tracker
	trackerConf, err := tracker.GetCurrentState()
	if err != nil {
		return err
	}

	c.Log.Info("Creating package manager...")
	packageManager, err := packagemanager.New(c.ContainerdSource, c.Log)
	if err != nil {
		return err
	}

	c.Log.Info("Setting package manager config", zap.Reflect("containerd source", string(c.ContainerdSource)))
	c.Log.Info("Configuring package manager. This might take a while...")
	if err := packageManager.Configure(ctx); err != nil {
		return err
	}

	c.Log.Info("Installing containerd...")
	if err := containerd.Install(ctx, trackerConf, packageManager, c.ContainerdSource); err != nil {
		return err
	}

	if err := containerd.ValidateSystemdUnitFile(); err != nil {
		return fmt.Errorf("please install systemd unit file for containerd: %v", err)
	}

	c.Log.Info("Installing iptables...")
	if err := iptables.Install(ctx, trackerConf, packageManager); err != nil {
		return err
	}

	switch c.CredentialProvider {
	case creds.IamRolesAnywhereCredentialProvider:
		c.Log.Info("Installing AWS signing helper...")
		if err := iamrolesanywhere.Install(ctx, trackerConf, c.AwsSource); err != nil {
			return err
		}
	case creds.SsmCredentialProvider:
		ssmInstaller := ssm.NewSSMInstaller(ssm.DefaultSsmInstallerRegion)

		c.Log.Info("Installing SSM agent installer...")
		if err := ssm.Install(ctx, trackerConf, ssmInstaller); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unable to detect hybrid auth method")
	}

	c.Log.Info("Installing kubelet...")
	if err := kubelet.Install(ctx, trackerConf, c.AwsSource); err != nil {
		return err
	}

	c.Log.Info("Installing kubectl...")
	if err := kubectl.Install(ctx, trackerConf, c.AwsSource); err != nil {
		return err
	}

	c.Log.Info("Installing cni-plugins...")
	if err := cni.Install(ctx, trackerConf, c.AwsSource); err != nil {
		return err
	}

	c.Log.Info("Installing image credential provider...")
	if err := imagecredentialprovider.Install(ctx, trackerConf, c.AwsSource); err != nil {
		return err
	}

	c.Log.Info("Installing IAM authenticator...")
	if err := iamauthenticator.Install(ctx, trackerConf, c.AwsSource); err != nil {
		return err
	}

	c.Log.Info("Finishing up install...")
	return trackerConf.Save()
}
