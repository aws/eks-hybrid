package s3

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	httpsScheme = "https"
	s3Scheme    = "s3"
)

func GetNodeadmURL(ctx context.Context, client *s3.Client, nodeadmUrl string) (string, error) {
	parsedURL, err := url.Parse(nodeadmUrl)
	if err != nil {
		return "", fmt.Errorf("parsing nodeadm URL: %v", err)
	}

	if parsedURL.Scheme != httpsScheme && parsedURL.Scheme != s3Scheme {
		return "", fmt.Errorf("invalid scheme. %s is not one of %v", parsedURL.Scheme, []string{httpsScheme, s3Scheme})
	}

	if parsedURL.Scheme == httpsScheme {
		return nodeadmUrl, nil
	}

	s3Bucket, s3BucketKey := extractBucketAndKey(parsedURL)

	preSignedURL, err := generatePreSignedURL(ctx, client, s3Bucket, s3BucketKey, 30*time.Minute)
	if err != nil {
		return "", fmt.Errorf("getting presigned URL for nodeadm: %v", err)
	}
	return preSignedURL, nil
}

func extractBucketAndKey(s3URL *url.URL) (bucket, key string) {
	parts := strings.SplitN(s3URL.Host, ".", 2)
	bucket = parts[0]
	key = strings.TrimPrefix(s3URL.Path, "/")
	return bucket, key
}

func generatePreSignedURL(ctx context.Context, client *s3.Client, bucket, key string, expiration time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(client)
	presignedUrl, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiration))
	if err != nil {
		return "", fmt.Errorf("generating pre-signed URL: %v", err)
	}
	return presignedUrl.URL, nil
}

func GeneratePutLogsPreSignedURL(ctx context.Context, client *s3.Client, bucket, key string, expiration time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(client)
	presignedUrl, err := presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiration))
	if err != nil {
		return "", fmt.Errorf("generating pre-signed logs upload URL: %v", err)
	}
	return presignedUrl.URL, nil
}
