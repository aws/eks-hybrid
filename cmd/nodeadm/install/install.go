package install

import (
	"context"
	"time"

	"github.com/integrii/flaggy"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/internal/containerd"
	"github.com/aws/eks-hybrid/internal/creds"
	"github.com/aws/eks-hybrid/internal/flows"
	"github.com/aws/eks-hybrid/internal/logger"
	"github.com/aws/eks-hybrid/internal/packagemanager"
	"github.com/aws/eks-hybrid/internal/ssm"
	"github.com/aws/eks-hybrid/internal/system"
)

const installHelpText = `Examples:
  # Install Kubernetes version 1.31 with AWS Systems Manager (SSM) as the credential provider
  nodeadm install 1.31 --credential-provider ssm

  # Install Kubernetes version 1.31 with AWS IAM Roles Anywhere as the credential provider and Docker as the containerd source
  nodeadm install 1.31 --credential-provider iam-ra --containerd-source docker

Documentation:
  https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-nodeadm.html#_install`

func NewCommand() cli.Command {
	cmd := command{
		timeout: 20 * time.Minute,
	}
	cmd.region = ssm.DefaultSsmInstallerRegion

	fc := flaggy.NewSubcommand("install")
	fc.Description = "Install components required to join an EKS cluster"
	fc.AdditionalHelpAppend = installHelpText
	fc.AddPositionalValue(&cmd.kubernetesVersion, "KUBERNETES_VERSION", 1, true, "The major[.minor[.patch]] version of Kubernetes to install.")
	fc.String(&cmd.credentialProvider, "p", "credential-provider", "Credential process to install. Allowed values: [ssm, iam-ra].")
	fc.String(&cmd.containerdSource, "s", "containerd-source", "Source for containerd artifact. Allowed values: [none, distro, docker].")
	fc.String(&cmd.region, "r", "region", "AWS region for downloading regional artifacts.")
	fc.Duration(&cmd.timeout, "t", "timeout", "Maximum install command duration. Input follows duration format. Example: 1h23s")
	cmd.flaggy = fc

	return &cmd
}

type command struct {
	flaggy             *flaggy.Subcommand
	kubernetesVersion  string
	credentialProvider string
	containerdSource   string
	region             string
	timeout            time.Duration
}

func (c *command) Flaggy() *flaggy.Subcommand {
	return c.flaggy
}

func (c *command) Run(log *zap.Logger, opts *cli.GlobalOptions) error {
	ctx := context.Background()
	ctx = logger.NewContext(ctx, log)

	root, err := cli.IsRunningAsRoot()
	if err != nil {
		return err
	}
	if !root {
		return cli.ErrMustRunAsRoot
	}

	if c.credentialProvider == "" {
		flaggy.ShowHelpAndExit("--credential-provider is a required flag. Allowed values are ssm & iam-ra")
	}
	credentialProvider, err := creds.GetCredentialProvider(c.credentialProvider)
	if err != nil {
		return err
	}
	osName, osVersion := system.GetOsNameWithVersion()
	if err = creds.ValidateCredentialProvider(credentialProvider, osName, osVersion); err != nil {
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

	log.Info("Creating package manager...")
	packageManager, err := packagemanager.New(containerdSource, log)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	log.Info("Validating Kubernetes version", zap.Reflect("kubernetes version", c.kubernetesVersion))
	// Create a Source for all AWS managed artifacts.
	awsSource, err := aws.GetLatestSource(ctx, c.kubernetesVersion)
	if err != nil {
		return err
	}
	log.Info("Using Kubernetes version", zap.Reflect("kubernetes version", awsSource.Eks.Version))

	installer := &flows.Installer{
		AwsSource:          awsSource,
		PackageManager:     packageManager,
		ContainerdSource:   containerdSource,
		SsmRegion:          c.region,
		CredentialProvider: credentialProvider,
		Logger:             log,
	}

	return installer.Run(ctx)
}
