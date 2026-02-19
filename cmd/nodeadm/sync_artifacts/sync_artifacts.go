package sync_artifacts

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
	"github.com/aws/eks-hybrid/internal/logger"
)

const syncArtifactsHelpText = `Examples:
  # Sync all Linux ARM64 dependencies for Kubernetes version 1.31 to S3
  nodeadm sync-artifacts 1.31 --arch arm64 --s3-bucket my-private-bucket --s3-prefix eks-deps/v1.31

  # Sync all Linux AMD64 dependencies for Kubernetes version 1.33 to S3
  nodeadm sync-artifacts 1.33 --arch arm64 --s3-bucket my-private-bucket --s3-prefix eks-deps/v1.33

  # Sync all Linux dependencies corresponding to host system's architecture to a non-default region in S3
  nodeadm sync-artifacts 1.34 --region ap-south-1 --s3-bucket my-private-bucket --s3-prefix eks-deps/ap-south-1/v1.34

Documentation:
  https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-nodeadm.html#_syncartifacts`

func NewCommand() cli.Command {
	cmd := command{
		timeout: 30 * time.Minute,
		arch:    runtime.GOARCH,
		os:      "linux",
		region:  "us-west-2",
	}

	fc := flaggy.NewSubcommand("sync-artifacts")
	fc.Description = "Sync EKS hybrid node dependencies directly to S3 for private installation"
	fc.AdditionalHelpAppend = syncArtifactsHelpText
	fc.AddPositionalValue(&cmd.kubernetesVersion, "KUBERNETES_VERSION", 1, true, "The major[.minor[.patch]] version of Kubernetes to sync dependencies for.")
	fc.String(&cmd.arch, "a", "arch", "Target architecture for artifacts.")
	fc.String(&cmd.region, "r", "region", "AWS region for downloading regional artifacts.")
	fc.String(&cmd.s3Bucket, "", "s3-bucket", "S3 bucket to sync the dependencies to (required).")
	fc.String(&cmd.s3Prefix, "", "s3-prefix", "S3 key prefix for the synced artifacts (required).")
	fc.Duration(&cmd.timeout, "t", "timeout", "Maximum sync command duration.")
	cmd.flaggy = fc

	return &cmd
}

type command struct {
	flaggy            *flaggy.Subcommand
	kubernetesVersion string
	arch              string
	os                string
	region            string
	s3Bucket          string
	s3Prefix          string
	timeout           time.Duration
}

func (c *command) Flaggy() *flaggy.Subcommand {
	return c.flaggy
}

func (c *command) Run(log *zap.Logger, opts *cli.GlobalOptions) error {
	ctx := context.Background()
	ctx = logger.NewContext(ctx, log)

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

	// Load AWS config once
	log.Info("Loading AWS configuration", zap.String("region", c.region))
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(c.region))
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 service client once
	svc := s3.NewFromConfig(cfg)

	// Validate S3 bucket exists and is accessible
	log.Info("Validating S3 bucket exists", zap.String("bucket", c.s3Bucket))
	if err := validateS3Bucket(ctx, svc, c.s3Bucket); err != nil {
		return fmt.Errorf("S3 bucket validation failed: %w", err)
	}
	log.Info("S3 bucket validation successful", zap.String("bucket", c.s3Bucket))

	downloader := &Downloader{
		AwsSource:     awsSource,
		Arch:          c.arch,
		OS:            c.os,
		Region:        c.region,
		S3Bucket:      c.s3Bucket,
		S3Prefix:      c.s3Prefix,
		Logger:        log,
		S3Client:      svc,
		SyncTimestamp: time.Now().Unix(),
	}

	return downloader.Run(ctx)
}

type Downloader struct {
	AwsSource     aws.Source
	Arch          string
	OS            string
	Region        string
	S3Bucket      string
	S3Prefix      string
	Logger        *zap.Logger
	S3Client      *s3.Client
	SyncTimestamp int64
}

type ArtifactInfo struct {
	Name        string
	URL         string
	ChecksumURL string
	LocalPath   string
}

func (d *Downloader) Run(ctx context.Context) error {
	d.Logger.Info("Starting dependency sync to S3",
		zap.String("s3Bucket", d.S3Bucket),
		zap.String("s3Prefix", d.S3Prefix),
		zap.String("arch", d.Arch),
		zap.String("os", d.OS),
	)

	artifacts, err := d.collectArtifacts()
	if err != nil {
		return errors.Wrap(err, "collecting artifacts")
	}

	d.Logger.Info("Found artifacts to sync", zap.Int("count", len(artifacts)))

	for _, artifact := range artifacts {
		if err := d.syncArtifactToS3(ctx, artifact); err != nil {
			return errors.Wrapf(err, "sync artifact %s", artifact.Name)
		}
	}

	// Generate local manifest with custom S3 URIs
	if err := d.generateCustomManifest(artifacts); err != nil {
		return errors.Wrap(err, "generating custom manifest")
	}

	d.Logger.Info("Successfully synced all dependencies to S3",
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

	// IAM Roles Anywhere artifacts
	if artifact := d.findArtifact(d.AwsSource.Iam.Artifacts, "aws_signing_helper"); artifact != nil {
		artifacts = append(artifacts, ArtifactInfo{
			Name:        "aws_signing_helper",
			URL:         artifact.URI,
			ChecksumURL: artifact.ChecksumURI,
			LocalPath:   "iam-ra/aws_signing_helper",
		})
	}

	// SSM artifacts
	installerURL, err := d.getSSMInstallerURL()
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

func (d *Downloader) syncArtifactToS3(ctx context.Context, artifact ArtifactInfo) error {
	d.Logger.Info("Syncing artifact to S3",
		zap.String("name", artifact.Name),
		zap.String("url", artifact.URL))

	// Download and upload main artifact
	s3Key := fmt.Sprintf("%s/%d/%s", strings.TrimSuffix(d.S3Prefix, "/"), d.SyncTimestamp, artifact.LocalPath)
	if err := d.streamToS3(ctx, d.S3Client, artifact.URL, s3Key); err != nil {
		return errors.Wrapf(err, "streaming %s to S3", artifact.Name)
	}

	// Download and upload checksum if available
	if artifact.ChecksumURL != "" {
		checksumKey := s3Key + ".sha256"
		if err := d.streamToS3(ctx, d.S3Client, artifact.ChecksumURL, checksumKey); err != nil {
			d.Logger.Warn("Failed to sync checksum to S3",
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

func (d *Downloader) getSSMInstallerURL() (string, error) {
	// Construct the URL directly using the same logic as SSM source
	variant, err := d.detectPlatformVariant()
	if err != nil {
		return "", err
	}

	dnsSuffix := d.AwsSource.RegionInfo.DnsSuffix

	platform := fmt.Sprintf("%v_%v", variant, d.Arch)
	return fmt.Sprintf("https://amazon-ssm-%v.s3.%v.%s/latest/%v/ssm-setup-cli", d.Region, d.Region, dnsSuffix, platform), nil
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
	dnsSuffix := d.AwsSource.RegionInfo.DnsSuffix

	// Create base S3 URL with partition-aware DNS suffix
	baseS3URL := fmt.Sprintf("https://%s.s3.%s.%s/%s/%d",
		d.S3Bucket, d.Region, dnsSuffix, strings.TrimSuffix(d.S3Prefix, "/"), d.SyncTimestamp)

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

	manifest := aws.Manifest{
		RegionConfig: aws.RegionConfig{
			d.Region: aws.RegionData{
				EcrAccountID: d.AwsSource.RegionInfo.EcrAccountID,
				CredProviders: map[string]bool{
					"iam-ra": d.AwsSource.RegionInfo.CredProviders["iam-ra"],
					"ssm":    d.AwsSource.RegionInfo.CredProviders["ssm"],
				},
			},
		},
		SsmReleases: ssmReleases,
		SupportedEksReleases: []aws.SupportedEksRelease{
			{
				MajorMinorVersion:  d.extractMajorMinor(d.AwsSource.Eks.Version),
				LatestPatchVersion: d.AwsSource.Eks.PatchVersion,
				PatchReleases: []aws.EksPatchRelease{
					{
						Version:      d.AwsSource.Eks.Version,
						PatchVersion: d.AwsSource.Eks.PatchVersion,
						ReleaseDate:  d.AwsSource.Eks.ReleaseDate,
						Artifacts:    eksArtifacts,
					},
				},
			},
		},
	}

	// Always add IAM releases if we have IAM artifacts
	if len(iamArtifacts) > 0 {
		manifest.IamRolesAnywhereReleases = []aws.IamRolesAnywhereRelease{
			{
				Version:   d.AwsSource.Iam.Version,
				Artifacts: iamArtifacts,
			},
		}
	}

	filename := fmt.Sprintf("manifest-%s-%s-%s-%d.yaml",
		d.AwsSource.Eks.Version, d.Arch, d.OS, d.SyncTimestamp)

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

// validateS3Bucket verifies that the specified S3 bucket exists and is accessible
func validateS3Bucket(ctx context.Context, svc *s3.Client, bucketName string) error {
	// Use HeadBucket to check if bucket exists and is accessible
	_, err := svc.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: awsSDKv2.String(bucketName),
	})
	if err != nil {
		// Check for specific error types to provide better error messages
		if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "NoSuchBucket") || strings.Contains(err.Error(), "MovedPermanently") {
			return fmt.Errorf("bucket '%s' does not exist", bucketName)
		}
		if strings.Contains(err.Error(), "Forbidden") || strings.Contains(err.Error(), "AccessDenied") {
			return fmt.Errorf("access denied to bucket '%s' - check your AWS credentials and bucket permissions", bucketName)
		}
	}

	return nil
}
