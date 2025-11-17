package download

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	awsSDKv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/integrii/flaggy"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/internal/cli"
	"github.com/aws/eks-hybrid/internal/creds"
	"github.com/aws/eks-hybrid/internal/logger"
	"github.com/aws/eks-hybrid/internal/ssm"
)

const downloadHelpText = `Examples:
  # Download all dependencies for Kubernetes version 1.31 with SSM credential provider
  nodeadm download 1.31 --credential-provider ssm --output /path/to/dependencies.tar.gz

  # Download all dependencies for Kubernetes version 1.31 with IAM Roles Anywhere
  nodeadm download 1.31 --credential-provider iam-ra --output ./eks-hybrid-deps.tar.gz --arch amd64

  # Download dependencies for specific region
  nodeadm download 1.31 --credential-provider ssm --region us-west-2 --output deps.tar.gz

  # Download and upload to S3 bucket
  nodeadm download 1.31 --credential-provider ssm --s3-bucket my-private-bucket --s3-key eks-deps/k8s-1.31-deps.tar.gz

Documentation:
  https://docs.aws.amazon.com/eks/latest/userguide/hybrid-nodes-nodeadm.html#_download`

func NewCommand() cli.Command {
	cmd := command{
		timeout: 30 * time.Minute,
		arch:    runtime.GOARCH,
		os:      runtime.GOOS,
		output:  "eks-hybrid-dependencies.tar.gz",
	}
	cmd.region = "us-east-1"

	fc := flaggy.NewSubcommand("download")
	fc.Description = "Download EKS hybrid node dependencies to a local tarball for private installation"
	fc.AdditionalHelpAppend = downloadHelpText
	fc.AddPositionalValue(&cmd.kubernetesVersion, "KUBERNETES_VERSION", 1, true, "The major[.minor[.patch]] version of Kubernetes to download dependencies for.")
	fc.String(&cmd.credentialProvider, "p", "credential-provider", "Credential process dependencies to include. Allowed values: [ssm, iam-ra].")
	fc.String(&cmd.arch, "a", "arch", "Target architecture for artifacts.")
	fc.String(&cmd.os, "o", "os", "Target operating system for artifacts.")
	fc.String(&cmd.region, "r", "region", "AWS region for downloading regional artifacts.")
	fc.String(&cmd.output, "", "output", "Output path for the dependencies tarball.")
	fc.String(&cmd.s3Bucket, "", "s3-bucket", "S3 bucket to upload the dependencies tarball to (optional).")
	fc.String(&cmd.s3Key, "", "s3-key", "S3 key/path for the uploaded tarball (required if s3-bucket is specified).")
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
	output             string
	s3Bucket           string
	s3Key              string
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

	// Validate S3 parameters
	if c.s3Bucket != "" && c.s3Key == "" {
		return fmt.Errorf("--s3-key is required when --s3-bucket is specified")
	}

	downloader := &Downloader{
		AwsSource:          awsSource,
		CredentialProvider: credentialProvider,
		Arch:               c.arch,
		OS:                 c.os,
		Region:             c.region,
		OutputPath:         c.output,
		S3Bucket:           c.s3Bucket,
		S3Key:              c.s3Key,
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
	OutputPath         string
	S3Bucket           string
	S3Key              string
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
	d.Logger.Info("Starting dependency download",
		zap.String("outputPath", d.OutputPath),
		zap.String("arch", d.Arch),
		zap.String("os", d.OS))

	// Create temporary directory for downloads
	tempDir, err := os.MkdirTemp("", "eks-hybrid-deps-*")
	if err != nil {
		return errors.Wrap(err, "creating temporary directory")
	}
	defer os.RemoveAll(tempDir)

	// Collect all artifacts to download
	artifacts, err := d.collectArtifacts()
	if err != nil {
		return errors.Wrap(err, "collecting artifacts")
	}

	d.Logger.Info("Found artifacts to download", zap.Int("count", len(artifacts)))

	// Download all artifacts
	for _, artifact := range artifacts {
		if err := d.downloadArtifact(ctx, tempDir, artifact); err != nil {
			return errors.Wrapf(err, "downloading artifact %s", artifact.Name)
		}
	}

	// Create the final tarball
	d.Logger.Info("Creating dependencies tarball", zap.String("path", d.OutputPath))
	if err := d.createTarball(tempDir, artifacts); err != nil {
		return errors.Wrap(err, "creating tarball")
	}

	d.Logger.Info("Successfully created dependencies tarball",
		zap.String("path", d.OutputPath),
		zap.Int("artifacts", len(artifacts)))

	// Upload to S3 if requested
	if d.S3Bucket != "" {
		if err := d.uploadToS3(ctx); err != nil {
			return errors.Wrap(err, "uploading to S3")
		}
		d.Logger.Info("Successfully uploaded dependencies to S3",
			zap.String("bucket", d.S3Bucket),
			zap.String("key", d.S3Key))
	}

	return nil
}

func (d *Downloader) collectArtifacts() ([]ArtifactInfo, error) {
	var artifacts []ArtifactInfo

	// EKS core artifacts
	eksArtifacts := []string{"kubelet", "kubectl", "cni-plugins", "image-credential-provider", "aws-iam-authenticator"}
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
			artifacts = append(artifacts, ArtifactInfo{
				Name:      "ssm-setup-cli",
				URL:       installerURL,
				LocalPath: "ssm/ssm-setup-cli",
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

func (d *Downloader) downloadArtifact(ctx context.Context, tempDir string, artifact ArtifactInfo) error {
	d.Logger.Info("Downloading artifact",
		zap.String("name", artifact.Name),
		zap.String("url", artifact.URL))

	// Create artifact directory
	artifactDir := filepath.Join(tempDir, filepath.Dir(artifact.LocalPath))
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return errors.Wrapf(err, "creating directory for %s", artifact.Name)
	}

	// Download main artifact
	artifactPath := filepath.Join(tempDir, artifact.LocalPath)
	if err := d.downloadFile(ctx, artifact.URL, artifactPath); err != nil {
		return errors.Wrapf(err, "downloading %s", artifact.Name)
	}

	// Download checksum if available
	if artifact.ChecksumURL != "" {
		checksumPath := artifactPath + ".checksum"
		if err := d.downloadFile(ctx, artifact.ChecksumURL, checksumPath); err != nil {
			d.Logger.Warn("Failed to download checksum",
				zap.String("artifact", artifact.Name),
				zap.Error(err))
		}
	}

	return nil
}

func (d *Downloader) downloadFile(ctx context.Context, url, dest string) error {
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

	file, err := os.Create(dest)
	if err != nil {
		return errors.Wrap(err, "creating file")
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	return errors.Wrap(err, "copying data")
}

func (d *Downloader) createTarball(tempDir string, artifacts []ArtifactInfo) error {
	file, err := os.Create(d.OutputPath)
	if err != nil {
		return errors.Wrap(err, "creating output file")
	}
	defer file.Close()

	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// Create metadata file
	metadata := d.createMetadata(artifacts)
	if err := d.addFileToTar(tarWriter, "metadata.yaml", []byte(metadata)); err != nil {
		return errors.Wrap(err, "adding metadata")
	}

	// Add all downloaded artifacts
	return filepath.Walk(tempDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(tempDir, path)
		if err != nil {
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return d.addFileToTar(tarWriter, relPath, data)
	})
}

func (d *Downloader) addFileToTar(tarWriter *tar.Writer, name string, data []byte) error {
	header := &tar.Header{
		Name: strings.ReplaceAll(name, "\\", "/"), // Ensure forward slashes
		Size: int64(len(data)),
		Mode: 0o644,
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		return err
	}

	_, err := tarWriter.Write(data)
	return err
}

func (d *Downloader) createMetadata(artifacts []ArtifactInfo) string {
	var sb strings.Builder
	sb.WriteString("# EKS Hybrid Dependencies Metadata\n")
	sb.WriteString(fmt.Sprintf("kubernetes_version: %s\n", d.AwsSource.Eks.Version))
	sb.WriteString(fmt.Sprintf("credential_provider: %s\n", d.CredentialProvider))
	sb.WriteString(fmt.Sprintf("architecture: %s\n", d.Arch))
	sb.WriteString(fmt.Sprintf("operating_system: %s\n", d.OS))
	sb.WriteString(fmt.Sprintf("region: %s\n", d.Region))
	sb.WriteString(fmt.Sprintf("created_at: %s\n", time.Now().UTC().Format(time.RFC3339)))
	sb.WriteString("artifacts:\n")

	for _, artifact := range artifacts {
		sb.WriteString(fmt.Sprintf("  - name: %s\n", artifact.Name))
		sb.WriteString(fmt.Sprintf("    path: %s\n", artifact.LocalPath))
		sb.WriteString(fmt.Sprintf("    source_url: %s\n", artifact.URL))
	}

	return sb.String()
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

func (d *Downloader) uploadToS3(ctx context.Context) error {
	d.Logger.Info("Uploading tarball to S3",
		zap.String("bucket", d.S3Bucket),
		zap.String("key", d.S3Key))

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(d.Region))
	if err != nil {
		return errors.Wrap(err, "loading AWS config")
	}

	// Create S3 service client
	svc := s3.NewFromConfig(cfg)

	// Open the tarball file
	file, err := os.Open(d.OutputPath)
	if err != nil {
		return errors.Wrap(err, "opening tarball file")
	}
	defer file.Close()

	// Upload to S3
	_, err = svc.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      awsSDKv2.String(d.S3Bucket),
		Key:         awsSDKv2.String(d.S3Key),
		Body:        file,
		ContentType: awsSDKv2.String("application/gzip"),
		Metadata: map[string]string{
			"kubernetes-version":  d.AwsSource.Eks.Version,
			"credential-provider": string(d.CredentialProvider),
			"architecture":        d.Arch,
			"operating-system":    d.OS,
			"region":              d.Region,
		},
	})

	return errors.Wrap(err, "uploading to S3")
}
