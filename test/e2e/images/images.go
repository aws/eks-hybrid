// Package images resolves the container image references used by the e2e tests.
//
// Test images are normally served out of the team's private ECR account through an ECR
// pull-through cache. For China regions we pull from public.ecr.aws directly instead.

package images

import (
	"fmt"

	awsinternal "github.com/aws/eks-hybrid/internal/aws"
	"github.com/aws/eks-hybrid/test/e2e/constants"
)

// Nginx returns the nginx image reference to use for the given region.
func Nginx(region, dnsSuffix, ecrAccount string) string {
	return testImage("nginx/nginx", region, dnsSuffix, ecrAccount)
}

// AwsCli returns the aws-cli image reference to use for the given region.
func AwsCli(region, dnsSuffix, ecrAccount string) string {
	return testImage("aws-cli/aws-cli", region, dnsSuffix, ecrAccount)
}

// testImage returns the reference for a repository mirrored from public.ecr.aws, using
// public ECR directly in China regions since the pull-through cache is unavailable there.
func testImage(repository, region, dnsSuffix, ecrAccount string) string {
	if IsChinaRegion(region) {
		return fmt.Sprintf("%s/%s:latest", constants.PublicEcrRegistry, repository)
	}
	return fmt.Sprintf("%s.dkr.ecr.%s.%s/ecr-public/%s:latest", ecrAccount, region, dnsSuffix, repository)
}

// IsChinaRegion returns true if the region belongs to the AWS China (aws-cn) partition.
func IsChinaRegion(region string) bool {
	return awsinternal.GetPartitionFromRegionFallback(region) == "aws-cn"
}
