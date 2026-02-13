package aws

import (
	"context"
	"os"

	"github.com/pkg/errors"
	"sigs.k8s.io/yaml"

	"github.com/aws/eks-hybrid/internal/util"
)

// set build time
var manifestUrl string

type Manifest struct {
	SupportedEksReleases     []SupportedEksRelease     `json:"supported_eks_releases"`
	IamRolesAnywhereReleases []IamRolesAnywhereRelease `json:"iam_roles_anywhere_releases"`
	SsmReleases              []SsmRelease              `json:"ssm_releases"`
	RegionConfig             RegionConfig              `json:"region_config"`
	PartitionConfig          PartitionConfig           `json:"partition_config,omitempty"`
}

type SupportedEksRelease struct {
	MajorMinorVersion  string            `json:"major_minor_version"`
	LatestPatchVersion string            `json:"latest_patch_version"`
	PatchReleases      []EksPatchRelease `json:"patch_releases"`
}

type EksPatchRelease struct {
	Version      string     `json:"version"`
	PatchVersion string     `json:"patch_version"`
	ReleaseDate  string     `json:"release_date"`
	Artifacts    []Artifact `json:"artifacts"`
}

type IamRolesAnywhereRelease struct {
	Version   string     `json:"version"`
	Artifacts []Artifact `json:"artifacts"`
}

type SsmRelease struct {
	Version   string     `json:"version"`
	Artifacts []Artifact `json:"artifacts"`
}

// RegionConfig represents the structure of the manifest file
type RegionConfig map[string]RegionData

// RegionData represents data for a specific region
type RegionData struct {
	Partition     string          `json:"partition,omitempty"`
	EcrAccountID  string          `json:"ecr_account_id"`
	CredProviders map[string]bool `json:"cred_providers"`
}

// PartitionConfig maps partition names to their DNS suffixes
// e.g., "aws" -> "amazonaws.com", "aws-cn" -> "amazonaws.com.cn"
type PartitionConfig map[string]string

type Artifact struct {
	Name        string `json:"name"`
	Arch        string `json:"arch"`
	OS          string `json:"os"`
	URI         string `json:"uri"`
	ChecksumURI string `json:"checksum_uri,omitempty"`
	GzipURI     string `json:"gzip_uri,omitempty"`
}

// Read from the manifest file on s3 and parse into Manifest struct
func getReleaseManifest(ctx context.Context) (*Manifest, error) {
	yamlFileData, err := util.GetHttpFile(ctx, manifestUrl)
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	err = yaml.Unmarshal(yamlFileData, &manifest)
	if err != nil {
		return nil, errors.Wrap(err, "invalid yaml data in release manifest")
	}
	return &manifest, nil
}

// GetPartition returns the partition for the region, using fallback if not in manifest
func (rd *RegionData) GetPartition(region string) string {
	if rd.Partition != "" {
		return rd.Partition
	}
	// Fallback to region-based detection
	return GetPartitionFromRegionFallback(region)
}

// GetDNSSuffix returns the DNS suffix for the region using manifest data with fallback
func (rd *RegionData) GetDNSSuffix(manifest *Manifest, region string) string {
	partition := rd.GetPartition(region)

	if manifest != nil && manifest.PartitionConfig != nil {
		if dnsSuffix, ok := manifest.PartitionConfig[partition]; ok && dnsSuffix != "" {
			return dnsSuffix
		}
	}

	return GetPartitionDNSSuffix(partition)
}

// GetPartitionFromRegionFallback determines the AWS partition based on the region prefix
// This is used as a fallback when partition info is not in the manifest
// Exported so it can be used by other packages like ECR
func GetPartitionFromRegionFallback(region string) string {
	if len(region) == 0 {
		return "aws"
	}

	// Check region prefixes to determine partition
	switch {
	case region[:3] == "cn-":
		return "aws-cn"
	case len(region) >= 7 && region[:7] == "us-gov-":
		return "aws-us-gov"
	case len(region) >= 7 && region[:7] == "us-iso-":
		return "aws-iso"
	case len(region) >= 8 && region[:8] == "us-isob-":
		return "aws-iso-b"
	case len(region) >= 8 && region[:8] == "us-isoe-":
		return "aws-iso-e"
	case len(region) >= 8 && region[:8] == "us-isof-":
		return "aws-iso-f"
	default:
		return "aws"
	}
}

// Read from a local manifest file and parse into Manifest struct
func getReleaseManifestFromFile(manifestPath string) (*Manifest, error) {
	yamlFileData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, errors.Wrapf(err, "reading manifest file %s", manifestPath)
	}
	var manifest Manifest
	err = yaml.Unmarshal(yamlFileData, &manifest)
	if err != nil {
		return nil, errors.Wrap(err, "invalid yaml data in release manifest")
	}
	return &manifest, nil
}
