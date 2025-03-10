package cleanup

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/addon"
	"github.com/aws/eks-hybrid/test/e2e/errors"
)

type S3Cleaner struct {
	s3Client *s3.Client
	logger   logr.Logger
}

func NewS3Cleaner(s3Client *s3.Client, logger logr.Logger) *S3Cleaner {
	return &S3Cleaner{
		s3Client: s3Client,
		logger:   logger,
	}
}

func (s *S3Cleaner) ListBuckets(ctx context.Context, filterInput FilterInput) ([]string, error) {
	paginator := s3.NewListBucketsPaginator(s.s3Client, &s3.ListBucketsInput{
		Prefix: aws.String(addon.PodIdentityS3BucketPrefix),
	})

	var bucketNames []string
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing buckets: %w", err)
		}
		for _, bucket := range output.Buckets {
			tags, err := s.s3Client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
				Bucket: bucket.Name,
			})
			if err != nil {
				return nil, fmt.Errorf("getting bucket tagging: %w", err)
			}

			if shouldDeleteBucket(&bucket, tags.TagSet, filterInput) {
				bucketNames = append(bucketNames, *bucket.Name)
			}

		}
	}

	return bucketNames, nil
}

func (s *S3Cleaner) DeleteBucket(ctx context.Context, bucketName string) error {
	if err := s.EmptyS3Bucket(ctx, bucketName); err != nil {
		return fmt.Errorf("emptying bucket %s: %w", bucketName, err)
	}

	_, err := s.s3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil && errors.IsType(err, &types.NoSuchBucket{}) {
		s.logger.Info("Bucket already deleted", "bucketName", bucketName)
		return nil
	}
	if err != nil {
		return fmt.Errorf("deleting bucket %s: %w", bucketName, err)
	}
	return nil
}

func (s *S3Cleaner) EmptyS3Bucket(ctx context.Context, bucketName string) error {
	output, err := s.s3Client.ListObjects(ctx, &s3.ListObjectsInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil && errors.IsType(err, &types.NoSuchBucket{}) {
		s.logger.Info("Bucket already deleted", "bucketName", bucketName)
		return nil
	}
	if err != nil {
		return fmt.Errorf("listing objects in bucket %s: %w", bucketName, err)
	}

	if len(output.Contents) == 0 {
		// no S3 objects to delete
		return nil
	}

	var s3Objects []types.ObjectIdentifier
	for _, content := range output.Contents {
		s3Objects = append(s3Objects, types.ObjectIdentifier{
			Key: content.Key,
		})
	}

	if _, err := s.s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(bucketName),
		Delete: &types.Delete{
			Objects: s3Objects,
		},
	}); err != nil {
		return fmt.Errorf("deleting objects in bucket %s: %w", bucketName, err)
	}

	return nil
}

func shouldDeleteBucket(bucket *types.Bucket, tags []types.Tag, input FilterInput) bool {
	var customTags []Tag
	for _, tag := range tags {
		customTags = append(customTags, Tag{
			Key:   *tag.Key,
			Value: *tag.Value,
		})
	}

	resource := ResourceWithTags{
		ID:           *bucket.Name,
		CreationTime: aws.ToTime(bucket.CreationDate),
		Tags:         customTags,
	}
	return shouldDeleteResource(resource, input)
}
