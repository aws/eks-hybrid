package peered

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/aws/eks-hybrid/test/e2e/addon"
	"github.com/aws/eks-hybrid/test/e2e/constants"
)

// PodIdentityBucket returns the pod identity bucket for the given cluster.
func PodIdentityBucket(ctx context.Context, client *s3.Client, cluster string) (string, error) {
	listBucketsOutput, err := client.ListBuckets(ctx, &s3.ListBucketsInput{
		Prefix: aws.String(addon.PodIdentityS3BucketPrefix),
	})
	if err != nil {
		return "", fmt.Errorf("listing buckets: %w", err)
	}

	for _, bucket := range listBucketsOutput.Buckets {
		getBucketTaggingOutput, err := client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
			Bucket: bucket.Name,
		})
		if err != nil {
			return "", fmt.Errorf("getting bucket tagging: %w", err)
		}

		var foundClusterTag, foundPodIdentityTag bool
		for _, tag := range getBucketTaggingOutput.TagSet {
			if *tag.Key == constants.TestClusterTagKey && *tag.Value == cluster {
				foundClusterTag = true
			}

			if *tag.Key == addon.PodIdentityS3BucketPrefix && *tag.Value == "true" {
				foundPodIdentityTag = true
			}

			if foundClusterTag && foundPodIdentityTag {
				return *bucket.Name, nil
			}
		}
	}
	return "", fmt.Errorf("Not Found")
}
