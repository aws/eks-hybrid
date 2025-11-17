package install

import (
	"context"
	"fmt"
	"os"
	"runtime"
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
	"github.com/aws/eks-hybrid/internal/private"
	"github.com/aws/eks-hybrid/internal/ssm"
	"github.com/aws/eks-hybrid/internal/tracker"
)

const installHelpText = `Examples:
  # Install Kubernetes version 1.31 with AWS Systems Manager (SSM) as the credential provider
  nodeadm install 1.31 --credential-provider ssm

  # Install Kubernetes version 1.31 with AWS IAM Roles Anywhere as the credential provider and Docker as the containerd source
  nodeadm install 1.31 --credential-provider iam-ra --containerd-source docker

  # Install from a private dependencies tarball (for air-gapped environments)
  nodeadm install 1.31 --credential-provider ssm --private-tarball /path/to/eks-hybrid-dependencies.tar.gz

  # Install from a private S3 bucket
  nodeadm install 1.31 --credential-provider ssm --s3-bucket my-private-bucket --s3-key eks-deps/k8s-1.31-deps.tar.gz

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
	fc.String(&cmd.privateTarball, "", "private-tarball", "Path to a local tarball containing pre-downloaded dependencies for private installation.")
	fc.String(&cmd.s3Bucket, "", "s3-bucket", "S3 bucket containing the dependencies tarball for private installation.")
	fc.String(&cmd.s3Key, "", "s3-key", "S3 key/path of the dependencies tarball (required if s3-bucket is specified).")
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
	privateTarball     string
	s3Bucket           string
	s3Key              string
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

	containerdSource, err := tracker.ContainerdSource(c.containerdSource)
	if err != nil {
		return err
	}
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

	var awsSource aws.Source
	var tarballSource *private.TarballSource

	// Validate private installation parameters
	if c.privateTarball != "" && c.s3Bucket != "" {
		return fmt.Errorf("cannot specify both --private-tarball and --s3-bucket, choose one")
	}
	if c.s3Bucket != "" && c.s3Key == "" {
		return fmt.Errorf("--s3-key is required when --s3-bucket is specified")
	}

	// Handle private installation vs normal installation
	if c.privateTarball != "" || c.s3Bucket != "" {
		var tarballPath string

		if c.s3Bucket != "" {
			log.Info("Downloading private tarball from S3",
				zap.String("bucket", c.s3Bucket),
				zap.String("key", c.s3Key))

			// Download from S3 to temporary file
			tarballPath, err = private.DownloadFromS3(ctx, c.s3Bucket, c.s3Key, c.region, log)
			if err != nil {
				return err
			}
			defer os.Remove(tarballPath)
		} else {
			tarballPath = c.privateTarball
			log.Info("Using private tarball for installation", zap.String("tarball", tarballPath))
		}

		// Create tarball source
		tarballSource, err = private.NewTarballSource(ctx, tarballPath, log)
		if err != nil {
			return err
		}
		if tarballSource.ExtractPath != "" {
			defer os.RemoveAll(tarballSource.ExtractPath)
		}

		// Validate compatibility
		if err := tarballSource.ValidateCompatibility(c.kubernetesVersion, c.credentialProvider, runtime.GOARCH, runtime.GOOS); err != nil {
			return err
		}

		// Create AWS source from tarball
		awsSource = tarballSource.CreateAWSSource()
		log.Info("Using Kubernetes version from tarball", zap.String("version", awsSource.Eks.Version))
	} else {
		log.Info("Validating Kubernetes version", zap.String("version", c.kubernetesVersion))
		// Create a Source for all AWS managed artifacts.
		awsSource, err = aws.GetLatestSource(ctx, c.kubernetesVersion, c.region)
		if err != nil {
			return err
		}
		log.Info("Using Kubernetes version", zap.String("version", awsSource.Eks.Version))
	}

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
