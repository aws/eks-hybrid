package hybrid

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/validation"
)

const (
	accessEntryRemediation = "Ensure your EKS cluster has at least one access entry of type HYBRID_LINUX with the hybrid node IAM role as principal."
)

// ValidateClusterAccess checks if the current IAM role has access to the EKS cluster
// through an access entry
func (hnp *HybridNodeProvider) ValidateClusterAccess(ctx context.Context, informer validation.Informer, _ *api.NodeConfig) error {
	var err error
	if hnp.awsConfig == nil {
		err = fmt.Errorf("AWS config not set")
		return err
	}

	if hnp.cluster == nil || hnp.cluster.Name == nil {
		informer.Starting(ctx, clusterAccessValidation, "Skipping cluster access validation due to node IAM role missing EKS DescribeCluster permission")
		informer.Done(ctx, clusterAccessValidation, err)
		return nil
	}

	informer.Starting(ctx, clusterAccessValidation, "Validating cluster access through EKS access entry")
	defer func() {
		informer.Done(ctx, clusterAccessValidation, err)
	}()

	stsClient := sts.NewFromConfig(*hnp.awsConfig)
	eksClient := eks.NewFromConfig(*hnp.awsConfig)

	getCallerIdentityOutput, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		err = validation.WithRemediation(fmt.Errorf("getting caller identity: %w", err), accessEntryRemediation)
		return err
	}

	if getCallerIdentityOutput.Arn == nil {
		err = validation.WithRemediation(fmt.Errorf("caller identity ARN is nil"), accessEntryRemediation)
		return err
	}

	roleArn := *getCallerIdentityOutput.Arn
	parsedARN, err := arn.Parse(roleArn)
	if err != nil {
		err = validation.WithRemediation(fmt.Errorf("parsing role ARN: %w", err), accessEntryRemediation)
		return err
	}

	roleName, ok := extractRoleNameFromARN(parsedARN)
	if !ok || roleName == "" {
		err = validation.WithRemediation(fmt.Errorf("extracting role name from ARN: %s", roleArn), accessEntryRemediation)
		return err
	}

	accessEntries, err := fetchAllAccessEntries(ctx, eksClient, hnp.cluster.Name)
	if err != nil {
		err = validation.WithRemediation(fmt.Errorf("fetching access entries from cluster: %w", err), accessEntryRemediation)
		return err
	}

	foundRole := false
	for _, accessEntry := range accessEntries {
		if strings.Contains(accessEntry, fmt.Sprintf("role/%s", roleName)) ||
			strings.Contains(accessEntry, fmt.Sprintf("role/%s/", roleName)) ||
			strings.HasSuffix(accessEntry, roleName) {
			foundRole = true
			break
		}
	}

	if !foundRole {
		err = validation.WithRemediation(
			fmt.Errorf("missing access entry of type HYBRID_LINUX with Hybrid Node role principal: %s", roleName),
			accessEntryRemediation,
		)
		return err
	}

	return nil
}

// extractRoleNameFromARN extracts the role name from an ARN
// Returns the role name and a boolean indicating if extraction was successful
func extractRoleNameFromARN(parsedARN arn.ARN) (string, bool) {
	splitArn := strings.Split(parsedARN.Resource, "/")

	// Handle assumed role ARN format: arn:aws:sts::123456789012:assumed-role/RoleName/session
	if parsedARN.Service == "sts" && strings.HasPrefix(parsedARN.Resource, "assumed-role") && len(splitArn) >= 2 {
		return splitArn[1], true
	}

	// Handle IAM role ARN format: arn:aws:iam::123456789012:role/RoleName
	if parsedARN.Service == "iam" && strings.HasPrefix(parsedARN.Resource, "role") && len(splitArn) >= 2 {
		return splitArn[len(splitArn)-1], true
	}

	return "", false
}

// fetchAllAccessEntries retrieves all access entries for a cluster with pagination handling
func fetchAllAccessEntries(ctx context.Context, eksClient *eks.Client, clusterName *string) ([]string, error) {
	accessEntries := []string{}
	var nextToken *string

	for {
		listAccessEntriesOutput, err := eksClient.ListAccessEntries(ctx, &eks.ListAccessEntriesInput{
			ClusterName: clusterName,
			NextToken:   nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list access entries: %w", err)
		}

		accessEntries = append(accessEntries, listAccessEntriesOutput.AccessEntries...)

		if listAccessEntriesOutput.NextToken == nil || aws.ToString(listAccessEntriesOutput.NextToken) == "" {
			break
		}
		nextToken = listAccessEntriesOutput.NextToken
	}

	return accessEntries, nil
}
