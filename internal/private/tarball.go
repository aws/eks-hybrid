package private

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	awsSDKv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sigs.k8s.io/yaml"

	"github.com/aws/eks-hybrid/internal/aws"
)

// TarballSource represents a local source of artifacts from a downloaded tarball
type TarballSource struct {
	ExtractPath string
	Metadata    *TarballMetadata
	Logger      *zap.Logger
}

// TarballMetadata represents the metadata stored in the tarball
type TarballMetadata struct {
	KubernetesVersion  string                `yaml:"kubernetes_version"`
	CredentialProvider string                `yaml:"credential_provider"`
	Architecture       string                `yaml:"architecture"`
	OperatingSystem    string                `yaml:"operating_system"`
	Region             string                `yaml:"region"`
	CreatedAt          string                `yaml:"created_at"`
	Artifacts          []TarballArtifactInfo `yaml:"artifacts"`
}

type TarballArtifactInfo struct {
	Name      string `yaml:"name"`
	Path      string `yaml:"path"`
	SourceURL string `yaml:"source_url"`
}

// NewTarballSource creates a new TarballSource by extracting the given tarball
func NewTarballSource(ctx context.Context, tarballPath string, logger *zap.Logger) (*TarballSource, error) {
	logger.Info("Setting up private installation from tarball", zap.String("path", tarballPath))

	// Create temporary extraction directory
	extractPath, err := os.MkdirTemp("", "eks-hybrid-private-*")
	if err != nil {
		return nil, errors.Wrap(err, "creating extraction directory")
	}

	// Extract tarball
	if err := extractTarball(tarballPath, extractPath); err != nil {
		os.RemoveAll(extractPath)
		return nil, errors.Wrap(err, "extracting tarball")
	}

	// Read metadata
	metadataPath := filepath.Join(extractPath, "metadata.yaml")
	metadata, err := readMetadata(metadataPath)
	if err != nil {
		os.RemoveAll(extractPath)
		return nil, errors.Wrap(err, "reading metadata")
	}

	logger.Info("Successfully extracted private installation tarball",
		zap.String("kubernetesVersion", metadata.KubernetesVersion),
		zap.String("credentialProvider", metadata.CredentialProvider),
		zap.Int("artifacts", len(metadata.Artifacts)))

	return &TarballSource{
		ExtractPath: extractPath,
		Metadata:    metadata,
		Logger:      logger,
	}, nil
}

// CreateAWSSource creates an aws.Source compatible with the existing installation flow
func (ts *TarballSource) CreateAWSSource() aws.Source {
	// Convert tarball artifacts to AWS artifacts format
	var eksArtifacts []aws.Artifact
	var iamArtifacts []aws.Artifact

	for _, artifact := range ts.Metadata.Artifacts {
		awsArtifact := aws.Artifact{
			Name:        artifact.Name,
			Arch:        ts.Metadata.Architecture,
			OS:          ts.Metadata.OperatingSystem,
			URI:         ts.getLocalPath(artifact.Path),
			ChecksumURI: ts.getLocalPath(artifact.Path + ".checksum"),
		}

		// Categorize artifacts based on their location in the tarball
		if strings.HasPrefix(artifact.Path, "eks/") {
			eksArtifacts = append(eksArtifacts, awsArtifact)
		} else if strings.HasPrefix(artifact.Path, "iam-ra/") {
			iamArtifacts = append(iamArtifacts, awsArtifact)
		}
		// Note: SSM artifacts are handled separately by the SSM installer flow
	}

	source := aws.Source{
		Eks: aws.EksPatchRelease{
			Version:   ts.Metadata.KubernetesVersion,
			Artifacts: eksArtifacts,
		},
		Iam: aws.IamRolesAnywhereRelease{
			Version:   "latest", // We don't track specific versions for IAM RA
			Artifacts: iamArtifacts,
		},
		RegionInfo: aws.RegionData{
			// We don't have region-specific configuration in the tarball
			// This could be expanded in the future if needed
		},
	}

	return source
}

// ValidateCompatibility checks if the tarball is compatible with the requested installation
func (ts *TarballSource) ValidateCompatibility(kubernetesVersion, credentialProvider, arch, osName string) error {
	if ts.Metadata.KubernetesVersion != kubernetesVersion {
		return fmt.Errorf("tarball Kubernetes version %s does not match requested version %s",
			ts.Metadata.KubernetesVersion, kubernetesVersion)
	}

	if ts.Metadata.CredentialProvider != credentialProvider {
		return fmt.Errorf("tarball credential provider %s does not match requested provider %s",
			ts.Metadata.CredentialProvider, credentialProvider)
	}

	if ts.Metadata.Architecture != arch {
		return fmt.Errorf("tarball architecture %s does not match system architecture %s",
			ts.Metadata.Architecture, arch)
	}

	if ts.Metadata.OperatingSystem != osName {
		return fmt.Errorf("tarball OS %s does not match system OS %s",
			ts.Metadata.OperatingSystem, osName)
	}

	return nil
}

func (ts *TarballSource) getLocalPath(relativePath string) string {
	return filepath.Join(ts.ExtractPath, relativePath)
}

func extractTarball(tarballPath, extractPath string) error {
	file, err := os.Open(tarballPath)
	if err != nil {
		return errors.Wrap(err, "opening tarball")
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return errors.Wrap(err, "creating gzip reader")
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Wrap(err, "reading tar header")
		}

		targetPath := filepath.Join(extractPath, header.Name)

		// Security check: ensure we don't extract outside the target directory
		if !strings.HasPrefix(targetPath, filepath.Clean(extractPath)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path in tarball: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return errors.Wrapf(err, "creating directory %s", targetPath)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return errors.Wrapf(err, "creating parent directory for %s", targetPath)
			}

			outFile, err := os.Create(targetPath)
			if err != nil {
				return errors.Wrapf(err, "creating file %s", targetPath)
			}

			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return errors.Wrapf(err, "extracting file %s", targetPath)
			}
			outFile.Close()

			// Set file permissions
			if err := os.Chmod(targetPath, os.FileMode(header.Mode)); err != nil {
				return errors.Wrapf(err, "setting permissions on %s", targetPath)
			}
		}
	}

	return nil
}

func readMetadata(metadataPath string) (*TarballMetadata, error) {
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, errors.Wrap(err, "reading metadata file")
	}

	var metadata TarballMetadata
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		return nil, errors.Wrap(err, "parsing metadata YAML")
	}

	return &metadata, nil
}

// DownloadFromS3 downloads a tarball from S3 to a temporary local file
func DownloadFromS3(ctx context.Context, bucket, key, region string, logger *zap.Logger) (string, error) {
	logger.Info("Downloading dependencies tarball from S3",
		zap.String("bucket", bucket),
		zap.String("key", key),
		zap.String("region", region))

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return "", errors.Wrap(err, "loading AWS config")
	}

	// Create S3 service client
	svc := s3.NewFromConfig(cfg)

	// Create temporary file
	tempFile, err := os.CreateTemp("", "eks-hybrid-deps-*.tar.gz")
	if err != nil {
		return "", errors.Wrap(err, "creating temporary file")
	}
	defer tempFile.Close()

	// Download from S3
	result, err := svc.GetObject(ctx, &s3.GetObjectInput{
		Bucket: awsSDKv2.String(bucket),
		Key:    awsSDKv2.String(key),
	})
	if err != nil {
		os.Remove(tempFile.Name())
		return "", errors.Wrap(err, "downloading from S3")
	}
	defer result.Body.Close()

	// Copy S3 object to temporary file
	_, err = io.Copy(tempFile, result.Body)
	if err != nil {
		os.Remove(tempFile.Name())
		return "", errors.Wrap(err, "copying S3 object to file")
	}

	logger.Info("Successfully downloaded dependencies tarball from S3",
		zap.String("localPath", tempFile.Name()))

	return tempFile.Name(), nil
}
