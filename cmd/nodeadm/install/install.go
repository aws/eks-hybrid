package install

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/integrii/flaggy"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sigs.k8s.io/yaml"

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

  # Install from a private installation using a custom manifest (for air-gapped environments)
  nodeadm install 1.31 --credential-provider ssm --manifest-override ./manifest-1.31.2-ssm-arm64-darwin.yaml --private-mode

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
	fc.String(&cmd.manifestOverride, "m", "manifest-override", "Path to a local manifest file containing custom artifact URLs for private installation.")
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

	// Validate private mode requirements
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

	log.Info("Creating package manager...")
	packageManager, err := packagemanager.New(containerdSource, log)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var awsSource aws.Source

	// Handle manifest override vs normal installation
	if c.manifestOverride != "" {
		log.Info("Using manifest override for private installation", zap.String("manifest", c.manifestOverride))

		awsSource, err = c.createSourceFromManifest(ctx, c.manifestOverride, credentialProvider)
		if err != nil {
			return err
		}
		log.Info("Using Kubernetes version from manifest", zap.String("version", awsSource.Eks.Version))
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
		PrivateMode:        c.privateMode, // Use privateMode flag
	}

	return installer.Run(ctx)
}

func (c *command) createSourceFromManifest(ctx context.Context, manifestPath string, credProvider creds.CredentialProvider) (aws.Source, error) {
	// Read the manifest file
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return aws.Source{}, errors.Wrapf(err, "reading manifest file %s", manifestPath)
	}

	// Parse the manifest
	var manifest aws.Manifest
	if err := yaml.Unmarshal(manifestData, &manifest); err != nil {
		return aws.Source{}, errors.Wrap(err, "parsing manifest YAML")
	}

	// Extract the region from the manifest (use the first available region)
	var regionData aws.RegionData
	var region string
	for r, data := range manifest.RegionConfig {
		region = r
		regionData = data
		break
	}
	if region == "" {
		return aws.Source{}, fmt.Errorf("no region configuration found in manifest")
	}

	// Find the appropriate EKS release
	var eksRelease aws.EksPatchRelease
	found := false
	for _, supportedRelease := range manifest.SupportedEksReleases {
		for _, patchRelease := range supportedRelease.PatchReleases {
			if patchRelease.Version == c.kubernetesVersion {
				eksRelease = patchRelease
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return aws.Source{}, fmt.Errorf("Kubernetes version %s not found in manifest", c.kubernetesVersion)
	}

	// Find the appropriate IAM release if using IAM Roles Anywhere
	var iamRelease aws.IamRolesAnywhereRelease
	if credProvider == creds.IamRolesAnywhereCredentialProvider {
		if len(manifest.IamRolesAnywhereReleases) == 0 {
			return aws.Source{}, fmt.Errorf("no IAM Roles Anywhere releases found in manifest for credential provider %s", credProvider)
		}
		iamRelease = manifest.IamRolesAnywhereReleases[0] // Use the first available release
	}

	// Create the AWS Source
	awsSource := aws.Source{
		Eks:        eksRelease,
		Iam:        iamRelease,
		RegionInfo: regionData,
	}

	return awsSource, nil
}
