package aws

import (
	"context"
	"fmt"

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
	if len(arn) < 6 || arn[0:4] != "arn:" {
		return "", fmt.Errorf("invalid ARN format: %s", arn)
	}

	// Find the second colon which marks the end of the partition
	partitionStart := 4
	partitionEnd := partitionStart
	for i := partitionStart; i < len(arn); i++ {
		if arn[i] == ':' {
			partitionEnd = i
			break
		}
	}

	if partitionEnd == partitionStart {
		return "", fmt.Errorf("partition not found in ARN: %s", arn)
	}

	return arn[partitionStart:partitionEnd], nil
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
	case len(region) >= 5 && region[:5] == "eusc-":
		return "aws-eusc"
	default:
		return "aws"
	}
}
