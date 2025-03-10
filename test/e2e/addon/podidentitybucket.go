package addon

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/aws/eks-hybrid/test/e2e/constants"
)

var PodIdentityBucketNotFound = errors.New("pod identity bucket not found")

// PodIdentityBucket returns the pod identity bucket for the given cluster.
func PodIdentityBucket(ctx context.Context, client *s3.Client, cluster string) (string, error) {
	listBucketsOutput, err := client.ListBuckets(ctx, &s3.ListBucketsInput{
		Prefix: aws.String(PodIdentityS3BucketPrefix),
	})
	if err != nil {
		return "", fmt.Errorf("listing buckets: %w", err)
	}

	var foundBuckets []string
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

			if *tag.Key == PodIdentityS3BucketPrefix && *tag.Value == "true" {
				foundPodIdentityTag = true
			}

			if foundClusterTag && foundPodIdentityTag {
				foundBuckets = append(foundBuckets, *bucket.Name)
			}
		}
	}

	if len(foundBuckets) > 1 {
		return "", fmt.Errorf("found multiple pod identity buckets for cluster %s: %v", cluster, foundBuckets)
	}

	if len(foundBuckets) == 0 {
		return "", PodIdentityBucketNotFound
	}

	return foundBuckets[0], nil
}
