package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// GetPartitionFromConfig determines the AWS partition from the AWS config
// by calling STS GetCallerIdentity and parsing the ARN
func GetPartitionFromConfig(ctx context.Context, cfg aws.Config) (string, error) {
	stsClient := sts.NewFromConfig(cfg)

	result, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("failed to get caller identity: %w", err)
	}

	if result.Arn == nil {
		return "", fmt.Errorf("caller identity ARN is nil")
	}

	// Parse partition from ARN (arn:partition:service:region:account-id:resource)
	partition, err := ParsePartitionFromARN(*result.Arn)
	if err != nil {
		return "", err
	}

	return partition, nil
}

// ParsePartitionFromARN extracts the partition from an ARN string
// ARN format: arn:partition:service:region:account-id:resource
func ParsePartitionFromARN(arn string) (string, error) {
	if !strings.HasPrefix(arn, "arn:") {
		return "", fmt.Errorf("invalid ARN format: %s", arn)
	}

	// Remove "arn:" prefix and find the partition (first field)
	remaining := strings.TrimPrefix(arn, "arn:")
	parts := strings.SplitN(remaining, ":", 2)

	if len(parts) == 0 || parts[0] == "" {
		return "", fmt.Errorf("partition not found in ARN: %s", arn)
	}

	return parts[0], nil
}

// GetPartitionDNSSuffix returns the DNS suffix for a given partition
func GetPartitionDNSSuffix(partition string) string {
	switch partition {
	case "aws":
		return "amazonaws.com"
	case "aws-cn":
		return "amazonaws.com.cn"
	case "aws-us-gov":
		return "amazonaws.com"
	case "aws-iso":
		return "c2s.ic.gov"
	case "aws-iso-b":
		return "sc2s.sgov.gov"
	case "aws-iso-e":
		return "cloud.adc-e.uk"
	case "aws-iso-f":
		return "csp.hci.ic.gov"
	case "aws-eusc":
		return "amazonaws.eu"
	default:
		// Default to standard AWS partition
		return "amazonaws.com"
	}
}

// GetServiceEndpointForPartition constructs service endpoints for different partitions
func GetServiceEndpointForPartition(service, region, partition string) string {
	dnsSuffix := GetPartitionDNSSuffix(partition)
	return fmt.Sprintf("%s.%s.%s", service, region, dnsSuffix)
}

// GetEC2ServicePrincipal returns the correct EC2 service principal for a partition.
// AWS China (aws-cn) uses ec2.amazonaws.com.cn
// All other partitions use ec2.amazonaws.com
func GetEC2ServicePrincipal(partition string) string {
	if partition == "aws-cn" {
		return "ec2.amazonaws.com.cn"
	}
	return "ec2.amazonaws.com"
}

// GetPartitionFromRegionFallback determines the AWS partition based on the region prefix
// This is used as a fallback when partition info is not in the manifest
func GetPartitionFromRegionFallback(region string) string {
	// Check region prefixes to determine partition
	switch {
	case strings.HasPrefix(region, "cn-"):
		return "aws-cn"
	case strings.HasPrefix(region, "us-gov-"):
		return "aws-us-gov"
	case strings.HasPrefix(region, "us-isob-"):
		return "aws-iso-b"
	case strings.HasPrefix(region, "us-isoe-"):
		return "aws-iso-e"
	case strings.HasPrefix(region, "us-isof-"):
		return "aws-iso-f"
	case strings.HasPrefix(region, "us-iso-"):
		return "aws-iso"
	case strings.HasPrefix(region, "eusc-"):
		return "aws-eusc"
	default:
		return "aws"
	}
}
