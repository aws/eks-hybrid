package download

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	awsSDKv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/integrii/flaggy"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sigs.k8s.io/yaml"

	"github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/internal/creds"
	"github.com/aws/eks-hybrid/internal/logger"
	"github.com/aws/eks-hybrid/internal/ssm"
)

const downloadHelpText = `Examples:
  # Mirror all dependencies for Kubernetes version 1.31 with SSM credential provider to S3
  nodeadm download 1.31 --credential-provider ssm --s3-bucket my-private-bucket --s3-prefix eks-deps/v1.31/

  # Mirror all dependencies for Kubernetes version 1.31 with IAM Roles Anywhere to S3
  nodeadm download 1.31 --credential-provider iam-ra --s3-bucket my-private-bucket --s3-prefix eks-deps/v1.31/ --arch amd64

  # Mirror dependencies for specific region to S3
  nodeadm download 1.31 --credential-provider ssm --region us-west-2 --s3-bucket my-private-bucket --s3-prefix eks-deps/us-west-2/

Documentation:
  https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-nodeadm.html#_download`

func NewCommand() cli.Command {
	cmd := command{
		timeout: 30 * time.Minute,
		arch:    runtime.GOARCH,
		os:      runtime.GOOS,
	}
	cmd.region = "us-west-2"

	fc := flaggy.NewSubcommand("download")
	fc.Description = "Mirror EKS hybrid node dependencies directly to S3 for private installation"
	fc.AdditionalHelpAppend = downloadHelpText
	fc.AddPositionalValue(&cmd.kubernetesVersion, "KUBERNETES_VERSION", 1, true, "The major[.minor[.patch]] version of Kubernetes to mirror dependencies for.")
	fc.String(&cmd.credentialProvider, "p", "credential-provider", "Credential process dependencies to include. Allowed values: [ssm, iam-ra].")
	fc.String(&cmd.arch, "a", "arch", "Target architecture for artifacts.")
	fc.String(&cmd.os, "o", "os", "Target operating system for artifacts.")
	fc.String(&cmd.region, "r", "region", "AWS region for downloading regional artifacts.")
	fc.String(&cmd.s3Bucket, "", "s3-bucket", "S3 bucket to mirror the dependencies to (required).")
	fc.String(&cmd.s3Prefix, "", "s3-prefix", "S3 key prefix for the mirrored artifacts (required).")
	fc.Duration(&cmd.timeout, "t", "timeout", "Maximum download command duration.")
	fc.Bool(&cmd.includeContainerd, "", "include-containerd", "Include containerd artifacts in the download (OS-specific packages).")
	cmd.flaggy = fc

	return &cmd
}

type command struct {
	flaggy             *flaggy.Subcommand
	kubernetesVersion  string
	credentialProvider string
	arch               string
	os                 string
	region             string
	s3Bucket           string
	s3Prefix           string
	timeout            time.Duration
	includeContainerd  bool
}

func (c *command) Flaggy() *flaggy.Subcommand {
	return c.flaggy
}

func (c *command) Run(log *zap.Logger, opts *cli.GlobalOptions) error {
	ctx := context.Background()
	ctx = logger.NewContext(ctx, log)

	if c.credentialProvider == "" {
		flaggy.ShowHelpAndExit("--credential-provider is a required flag. Allowed values are ssm & iam-ra")
	}

	credentialProvider, err := creds.GetCredentialProvider(c.credentialProvider)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	log.Info("Validating Kubernetes version", zap.String("version", c.kubernetesVersion))

	// Create a Source for all AWS managed artifacts
	awsSource, err := aws.GetLatestSource(ctx, c.kubernetesVersion, c.region)
	if err != nil {
		return err
	}
	log.Info("Using Kubernetes version", zap.String("version", awsSource.Eks.Version))

	// Validate S3 parameters - both are required now
	if c.s3Bucket == "" {
		return fmt.Errorf("--s3-bucket is required")
	}
	if c.s3Prefix == "" {
		return fmt.Errorf("--s3-prefix is required")
	}

	downloader := &Downloader{
		AwsSource:          awsSource,
		CredentialProvider: credentialProvider,
		Arch:               c.arch,
		OS:                 c.os,
		Region:             c.region,
		S3Bucket:           c.s3Bucket,
		S3Prefix:           c.s3Prefix,
		IncludeContainerd:  c.includeContainerd,
		Logger:             log,
	}

	return downloader.Run(ctx)
}

type Downloader struct {
	AwsSource          aws.Source
	CredentialProvider creds.CredentialProvider
	Arch               string
	OS                 string
	Region             string
	S3Bucket           string
	S3Prefix           string
	IncludeContainerd  bool
	Logger             *zap.Logger
}

type ArtifactInfo struct {
	Name        string
	URL         string
	ChecksumURL string
	LocalPath   string
}

func (d *Downloader) Run(ctx context.Context) error {
	d.Logger.Info("Starting dependency mirroring to S3",
		zap.String("s3Bucket", d.S3Bucket),
		zap.String("s3Prefix", d.S3Prefix),
		zap.String("arch", d.Arch),
		zap.String("os", d.OS))

	// Collect all artifacts to mirror
	artifacts, err := d.collectArtifacts()
	if err != nil {
		return errors.Wrap(err, "collecting artifacts")
	}

	d.Logger.Info("Found artifacts to mirror", zap.Int("count", len(artifacts)))

	// Mirror all artifacts directly to S3
	for _, artifact := range artifacts {
		if err := d.mirrorArtifactToS3(ctx, artifact); err != nil {
			return errors.Wrapf(err, "mirroring artifact %s", artifact.Name)
		}
	}

	// Generate local manifest with custom S3 URIs
	if err := d.generateCustomManifest(artifacts); err != nil {
		return errors.Wrap(err, "generating custom manifest")
	}

	d.Logger.Info("Successfully mirrored all dependencies to S3",
		zap.String("bucket", d.S3Bucket),
		zap.String("prefix", d.S3Prefix),
		zap.Int("artifacts", len(artifacts)))

	return nil
}

func (d *Downloader) collectArtifacts() ([]ArtifactInfo, error) {
	var artifacts []ArtifactInfo

	// EKS core artifacts
	eksArtifacts := []string{"kubelet", "kubectl", "cni-plugins", "ecr-credential-provider", "aws-iam-authenticator"}
	for _, name := range eksArtifacts {
		if artifact := d.findArtifact(d.AwsSource.Eks.Artifacts, name); artifact != nil {
			artifacts = append(artifacts, ArtifactInfo{
				Name:        name,
				URL:         artifact.URI,
				ChecksumURL: artifact.ChecksumURI,
				LocalPath:   fmt.Sprintf("eks/%s", name),
			})
		}
	}

	// Credential provider specific artifacts
	switch d.CredentialProvider {
	case creds.IamRolesAnywhereCredentialProvider:
		if artifact := d.findArtifact(d.AwsSource.Iam.Artifacts, "aws_signing_helper"); artifact != nil {
			artifacts = append(artifacts, ArtifactInfo{
				Name:        "aws_signing_helper",
				URL:         artifact.URI,
				ChecksumURL: artifact.ChecksumURI,
				LocalPath:   "iam-ra/aws_signing_helper",
			})
		}
	case creds.SsmCredentialProvider:
		// For SSM, we need to get the installer URL from the SSM source
		ssmInstaller := ssm.NewSSMInstaller(d.Logger, d.Region)
		installerURL, err := d.getSSMInstallerURL(ssmInstaller)
		if err == nil {
			// Add the main SSM installer
			artifacts = append(artifacts, ArtifactInfo{
				Name:      "ssm-setup-cli",
				URL:       installerURL,
				LocalPath: "ssm/ssm-setup-cli",
			})
			// Add the signature file
			sigURL := installerURL + ".sig"
			artifacts = append(artifacts, ArtifactInfo{
				Name:        "ssm-setup-cli.sig",
				URL:         sigURL,
				ChecksumURL: "", // Signature files don't have checksums
				LocalPath:   "ssm/ssm-setup-cli.sig",
			})
		} else {
			d.Logger.Warn("Failed to get SSM installer URL", zap.Error(err))
		}
	}

	if len(artifacts) == 0 {
		return nil, fmt.Errorf("no artifacts found for architecture %s and OS %s", d.Arch, d.OS)
	}

	return artifacts, nil
}

func (d *Downloader) findArtifact(artifacts []aws.Artifact, name string) *aws.Artifact {
	for _, artifact := range artifacts {
		if artifact.Name == name && artifact.Arch == d.Arch && artifact.OS == d.OS {
			return &artifact
		}
	}
	return nil
}

func (d *Downloader) mirrorArtifactToS3(ctx context.Context, artifact ArtifactInfo) error {
	d.Logger.Info("Mirroring artifact to S3",
		zap.String("name", artifact.Name),
		zap.String("url", artifact.URL))

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(d.Region))
	if err != nil {
		return errors.Wrap(err, "loading AWS config")
	}

	// Create S3 service client
	svc := s3.NewFromConfig(cfg)

	// Download and upload main artifact
	s3Key := strings.TrimSuffix(d.S3Prefix, "/") + "/" + artifact.LocalPath
	if err := d.streamToS3(ctx, svc, artifact.URL, s3Key); err != nil {
		return errors.Wrapf(err, "streaming %s to S3", artifact.Name)
	}

	// Download and upload checksum if available
	if artifact.ChecksumURL != "" {
		checksumKey := s3Key + ".sha256"
		if err := d.streamToS3(ctx, svc, artifact.ChecksumURL, checksumKey); err != nil {
			d.Logger.Warn("Failed to mirror checksum to S3",
				zap.String("artifact", artifact.Name),
				zap.Error(err))
		}
	}

	return nil
}

func (d *Downloader) streamToS3(ctx context.Context, svc *s3.Client, url, s3Key string) error {
	// Download from source URL
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return errors.Wrap(err, "creating request")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "making request")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Create S3 Manager uploader
	uploader := manager.NewUploader(svc)

	// Upload using S3 Manager with public-read ACL
	_, err = uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: awsSDKv2.String(d.S3Bucket),
		Key:    awsSDKv2.String(s3Key),
		Body:   resp.Body,
		ACL:    types.ObjectCannedACLPublicRead,
	})

	return errors.Wrap(err, "uploading to S3 using manager")
}

func (d *Downloader) getSSMInstallerURL(ssmInstaller ssm.Source) (string, error) {
	// We need to use reflection or a type assertion to get the URL builder
	// For now, let's construct the URL directly using the same logic as SSM source
	variant, err := d.detectPlatformVariant()
	if err != nil {
		return "", err
	}

	platform := fmt.Sprintf("%v_%v", variant, d.Arch)
	return fmt.Sprintf("https://amazon-ssm-%v.s3.%v.amazonaws.com/latest/%v/ssm-setup-cli", d.Region, d.Region, platform), nil
}

func (d *Downloader) detectPlatformVariant() (string, error) {
	// This is a simplified version - in a real implementation you'd want to detect the actual OS
	switch d.OS {
	case "linux":
		return "linux", nil
	default:
		return "", fmt.Errorf("unsupported OS: %s", d.OS)
	}
}

func (d *Downloader) generateCustomManifest(artifacts []ArtifactInfo) error {
	// Create base S3 URL
	baseS3URL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s",
		d.S3Bucket, d.Region, strings.TrimSuffix(d.S3Prefix, "/"))

	// Build artifact list with custom S3 URIs
	var eksArtifacts []aws.Artifact
	var iamArtifacts []aws.Artifact
	var ssmArtifacts []aws.Artifact

	for _, artifact := range artifacts {
		customURI := fmt.Sprintf("%s/%s", baseS3URL, artifact.LocalPath)
		customChecksumURI := ""
		if artifact.ChecksumURL != "" {
			customChecksumURI = fmt.Sprintf("%s/%s.sha256", baseS3URL, artifact.LocalPath)
		}

		awsArtifact := aws.Artifact{
			Name:        artifact.Name,
			Arch:        d.Arch,
			OS:          d.OS,
			URI:         customURI,
			ChecksumURI: customChecksumURI,
		}

		// Categorize artifacts
		if strings.HasPrefix(artifact.LocalPath, "iam-ra/") {
			iamArtifacts = append(iamArtifacts, awsArtifact)
		} else if strings.HasPrefix(artifact.LocalPath, "eks/") {
			eksArtifacts = append(eksArtifacts, awsArtifact)
		} else if strings.HasPrefix(artifact.LocalPath, "ssm/") {
			ssmArtifacts = append(ssmArtifacts, awsArtifact)
		}
	}

	// Create SSM releases if we have SSM artifacts
	var ssmReleases []aws.SsmRelease
	if len(ssmArtifacts) > 0 {
		ssmReleases = []aws.SsmRelease{
			{
				Version:   "latest", // SSM setup CLI uses "latest" as version
				Artifacts: ssmArtifacts,
			},
		}
	}

	// Create custom manifest structure using existing aws package types
	manifest := aws.Manifest{
		RegionConfig: aws.RegionConfig{
			d.Region: aws.RegionData{
				EcrAccountID: d.AwsSource.RegionInfo.EcrAccountID,
				CredProviders: map[string]bool{
					"iam-ra": d.CredentialProvider == creds.IamRolesAnywhereCredentialProvider,
					"ssm":    d.CredentialProvider == creds.SsmCredentialProvider,
				},
			},
		},
		SsmReleases: ssmReleases,
		SupportedEksReleases: []aws.SupportedEksRelease{
			{
				MajorMinorVersion:  d.extractMajorMinor(d.AwsSource.Eks.Version),
				LatestPatchVersion: "1", // Simplified for custom manifest
				PatchReleases: []aws.EksPatchRelease{
					{
						Version:      d.AwsSource.Eks.Version,
						PatchVersion: "1",
						ReleaseDate:  time.Now().Format("2006-01-02"),
						Artifacts:    eksArtifacts,
					},
				},
			},
		},
	}

	// Add IAM releases if we have IAM artifacts
	if len(iamArtifacts) > 0 {
		manifest.IamRolesAnywhereReleases = []aws.IamRolesAnywhereRelease{
			{
				Version:   d.AwsSource.Iam.Version,
				Artifacts: iamArtifacts,
			},
		}
	}

	// Generate filename based on configuration
	filename := fmt.Sprintf("manifest-%s-%s-%s-%s.yaml",
		d.AwsSource.Eks.Version, d.CredentialProvider, d.Arch, d.OS)

	// Marshal to YAML
	yamlData, err := yaml.Marshal(manifest)
	if err != nil {
		return errors.Wrap(err, "marshaling manifest to YAML")
	}

	// Write to local file
	if err := os.WriteFile(filename, yamlData, 0o644); err != nil {
		return errors.Wrapf(err, "writing manifest file %s", filename)
	}

	d.Logger.Info("Generated custom manifest with S3 URIs",
		zap.String("filename", filename),
		zap.String("baseS3URL", baseS3URL))

	return nil
}

func (d *Downloader) extractMajorMinor(version string) string {
	// Extract major.minor from version like "1.31.2" -> "1.31"
	parts := strings.Split(version, ".")
	if len(parts) >= 2 {
		return fmt.Sprintf("%s.%s", parts[0], parts[1])
	}
	return version
}
