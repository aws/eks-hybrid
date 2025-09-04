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

func (hnp *HybridNodeProvider) ValidateClusterAccess(ctx context.Context, informer validation.Informer, _ *api.NodeConfig) error {
	var err error
	informer.Starting(ctx, "cluster-access", "Validating cluster access through EKS access entry or ConfigMap")
	defer func() {
		informer.Done(ctx, "cluster-access", err)
	}()

	var roleName string
	stsClient := sts.NewFromConfig(*hnp.awsConfig)
	eksClient := eks.NewFromConfig(*hnp.awsConfig)

	getCallerIdentityOutput, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return err
	}
	roleArn := *getCallerIdentityOutput.Arn
	parsedARN, err := arn.Parse(roleArn)
	if err != nil {
		return err
	}

	splitArn := strings.Split(parsedARN.Resource, "/")
	if parsedARN.Service == "sts" && strings.HasPrefix(parsedARN.Resource, "assumed-role") {
		roleName = splitArn[1]
	} else if parsedARN.Service == "iam" && strings.HasPrefix(parsedARN.Resource, "role") {
		roleName = splitArn[len(splitArn)-1]
	}

	accessEntries := []string{}
	listAccessEntriesOutput, err := eksClient.ListAccessEntries(ctx, &eks.ListAccessEntriesInput{
		ClusterName: hnp.cluster.Name,
	})
	if err != nil {
		return err
	}

	accessEntries = append(accessEntries, listAccessEntriesOutput.AccessEntries...)
	nextToken := listAccessEntriesOutput.NextToken

	for nextToken != nil && aws.ToString(nextToken) != "" {
		listAccessEntriesOutput, err = eksClient.ListAccessEntries(ctx, &eks.ListAccessEntriesInput{
			ClusterName: hnp.cluster.Name,
			NextToken:   nextToken,
		})
		if err != nil {
			return err
		}

		accessEntries = append(accessEntries, listAccessEntriesOutput.AccessEntries...)
		nextToken = listAccessEntriesOutput.NextToken
	}

	foundRole := false
	for _, accessEntry := range accessEntries {
		if strings.Contains(accessEntry, roleName) {
			foundRole = true
		}
	}

	if !foundRole {
		err = validation.WithRemediation(fmt.Errorf("missing access entry with Hybrid Node role principal"), "Ensure your EKS cluster has at least one access entry with the hybrid node IAM role as principal.")
		return err
	}

	return nil
}
