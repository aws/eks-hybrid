package install

import (
	"context"
	"fmt"
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
	"github.com/aws/eks-hybrid/internal/tracker"
)

const installHelpText = `Examples:
  # Install Kubernetes version 1.31 with AWS Systems Manager (SSM) as the credential provider
  nodeadm install 1.31 --credential-provider ssm

  # Install Kubernetes version 1.31 with AWS IAM Roles Anywhere as the credential provider and Docker as the containerd source
  nodeadm install 1.31 --credential-provider iam-ra --containerd-source docker

  # Install from a private installation using a local custom manifest (for air-gapped environments)
  nodeadm install 1.31 --credential-provider ssm --manifest-override file://./manifest-1.31.13-arm64-linux-1765487946.yaml --private-mode

  # Install from a private installation using a remote custom manifest
  nodeadm install 1.31 --credential-provider ssm --manifest-override https://my-bucket.s3.us-west-2.amazonaws.com/manifests/manifest.yaml --private-mode

Documentation:
  https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-nodeadm.html#_install`

func NewCommand() cli.Command {
	cmd := command{
		timeout:          20 * time.Minute,
		containerdSource: string(tracker.ContainerdSourceDistro),
	}
	cmd.region = ssm.DefaultSsmInstallerRegion

	fc := flaggy.NewSubcommand("install")
	fc.Description = "Install components required to join an EKS cluster"
	fc.AdditionalHelpAppend = installHelpText
	fc.AddPositionalValue(&cmd.kubernetesVersion, "KUBERNETES_VERSION", 1, true, "The major[.minor[.patch]] version of Kubernetes to install.")
	fc.String(&cmd.credentialProvider, "p", "credential-provider", "Credential process to install. Allowed values: [ssm, iam-ra].")
	fc.String(&cmd.containerdSource, "s", "containerd-source", "Source for containerd artifact. Allowed values: [none, distro, docker].")
	fc.String(&cmd.region, "r", "region", "AWS region for downloading regional artifacts.")
	fc.String(&cmd.manifestOverride, "m", "manifest-override", "URI to a manifest file containing custom artifact URLs. Supports file:// for local files and https:// for remote files.")
	fc.Bool(&cmd.privateMode, "", "private-mode", "Enable private installation mode (skips OS packages, requires --manifest-override).")
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
	manifestOverride   string
	privateMode        bool
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

	if c.privateMode && c.manifestOverride == "" {
		return fmt.Errorf("--private-mode requires --manifest-override to be specified")
	}

	credentialProvider, err := creds.GetCredentialProvider(c.credentialProvider)
	if err != nil {
		return err
	}

	containerdSource, err := tracker.ContainerdSource(c.containerdSource)
	if err != nil {
		return err
	}
	if err := containerd.ValidateContainerdSource(containerdSource); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var awsSource aws.Source
	var packageManager *packagemanager.DistroPackageManager

	// Use manifest override if provided, otherwise use default AWS source
	if c.manifestOverride != "" {
		log.Info("Using manifest override", zap.String("manifest", c.manifestOverride))
		awsSource, err = aws.GetLatestSourceFromManifest(ctx, c.kubernetesVersion, c.region, c.manifestOverride)
		if err != nil {
			return err
		}
		log.Info("Using Kubernetes version from manifest", zap.String("version", awsSource.Eks.Version))
	} else {
		log.Info("Validating Kubernetes version", zap.Reflect("kubernetes version", c.kubernetesVersion))
		// Create a Source for all AWS managed artifacts.
		awsSource, err = aws.GetLatestSource(ctx, c.kubernetesVersion, c.region)
		if err != nil {
			return err
		}
		log.Info("Using Kubernetes version", zap.String("version", awsSource.Eks.Version))
	}

	// Create package manager unless in private mode
	if !c.privateMode {
		log.Info("Creating package manager...")
		packageManager, err = packagemanager.New(containerdSource, log)
		if err != nil {
			return err
		}
	}

	installer := &flows.Installer{
		AwsSource:          awsSource,
		PackageManager:     packageManager,
		ContainerdSource:   containerdSource,
		SsmRegion:          c.region,
		CredentialProvider: credentialProvider,
		Logger:             log,
		PrivateMode:        c.privateMode,
	}

	return installer.Run(ctx)
}
