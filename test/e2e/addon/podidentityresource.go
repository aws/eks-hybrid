package addon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/aws/eks-hybrid/test/e2e/constants"
	e2eErrors "github.com/aws/eks-hybrid/test/e2e/errors"
)

var ErrPodIdentityBucketNotFound = errors.New("pod identity bucket not found")

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
		if err != nil && (e2eErrors.IsS3BucketNotFound(err) || e2eErrors.IsAwsError(err, "NoSuchTagSet")) {
			// We have to pull all buckets and then get the tags
			// the bucket could get deleted between the list and get tags call
			continue
		}
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
		return "", ErrPodIdentityBucketNotFound
	}

	return foundBuckets[0], nil
}

// PodIdentityRole returns the pod identity role ARN for the given cluster.
func PodIdentityRole(ctx context.Context, client *iam.Client, cluster string) (string, error) {
	paginator := iam.NewListRolesPaginator(client, &iam.ListRolesInput{
		PathPrefix: aws.String(constants.TestRolePathPrefix),
	})

	var foundRoles []string
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return "", fmt.Errorf("listing roles: %w", err)
		}

		for _, role := range output.Roles {
			// Get role tags
			getRoleTagsOutput, err := client.ListRoleTags(ctx, &iam.ListRoleTagsInput{
				RoleName: role.RoleName,
			})
			if e2eErrors.IsIAMRoleNotFound(err) {
				// role may have been deleted after list
				continue
			}
			if err != nil {
				return "", fmt.Errorf("getting role tags: %w", err)
			}

			var foundClusterTag bool
			for _, tag := range getRoleTagsOutput.Tags {
				if *tag.Key == constants.TestClusterTagKey && *tag.Value == cluster {
					foundClusterTag = true
					break
				}
			}

			if foundClusterTag {
				// Verify this is the pod identity role by checking the trust relationship
				getRoleOutput, err := client.GetRole(ctx, &iam.GetRoleInput{
					RoleName: role.RoleName,
				})
				if e2eErrors.IsIAMRoleNotFound(err) {
					continue
				}
				if err != nil {
					return "", fmt.Errorf("getting role: %w", err)
				}

				foundRole, err := isPodIdentityRole(*getRoleOutput.Role.AssumeRolePolicyDocument)
				if err != nil {
					return "", fmt.Errorf("failed to check if role %s is pod identity role", *role.RoleName)
				}
				// Check if this role has the expected trust relationship for pod identity
				// if err is returned, we can
				if foundRole {
					foundRoles = append(foundRoles, *role.Arn)
				}
			}
		}
	}

	if len(foundRoles) > 1 {
		return "", fmt.Errorf("found multiple pod identity roles for cluster %s: %v", cluster, foundRoles)
	}

	if len(foundRoles) == 0 {
		return "", fmt.Errorf("pod identity role not found for cluster %s", cluster)
	}

	return foundRoles[0], nil
}

// isPodIdentityRole checks if the given policy document is for a pod identity role
func isPodIdentityRole(policyDoc string) (bool, error) {
	// The policy document is URL encoded, so we need to decode it first
	decodedDoc, err := url.QueryUnescape(policyDoc)
	if err != nil {
		return false, err
	}

	policy := &PolicyDocument{}

	if err := json.Unmarshal([]byte(decodedDoc), &policy); err != nil {
		return false, err
	}

	// Check if this is a pod identity role by looking for the expected service principal
	// and actions in the trust relationship
	for _, statement := range policy.Statement {
		if statement.Effect != "Allow" {
			continue
		}
		for key, val := range statement.Principal {
			if key != "Service" {
				continue
			}
			if val != "pods.eks.amazonaws.com" || !strings.HasSuffix(val, ".pods.eks.aws.internal") {
				continue
			}

			for _, action := range statement.Action {
				if action == "sts:AssumeRole" || action == "sts:TagSession" {
					return true, nil
				}
			}
		}
	}

	return false, nil
}
