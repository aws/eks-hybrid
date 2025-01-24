package credentials

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e"
)

// Infrastructure represents the necessary infrastructure for credentials providers to be used by nodeadm.
type Infrastructure struct {
	StackOutput
	RolesAnywhereCA *Certificate

	stack  *Stack
	logger logr.Logger
}

// Setup creates the necessary infrastructure for credentials providers to be used by nodeadm. Endpoint is
// used by EKS client and will use default endpoint if an empty string is passed.
func Setup(ctx context.Context, logger logr.Logger, config aws.Config, clusterName, endpoint string) (*Infrastructure, error) {
	eksClient := eks.NewFromConfig(config, func(o *eks.Options) {
		o.EndpointResolverV2 = &e2e.EksResolverV2{Endpoint: endpoint}
	})
	cfnClient := cloudformation.NewFromConfig(config)
	iamClient := iam.NewFromConfig(config)

	cluster, err := eksClient.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	})
	if err != nil {
		return nil, fmt.Errorf("getting cluster details: %w", err)
	}

	rolesAnywhereCA, err := CreateCA()
	if err != nil {
		return nil, err
	}

	stackName := fmt.Sprintf("EKSHybridCI-%s", e2e.SanitizeForAWSName(clusterName))
	stack := &Stack{
		ClusterName:            *cluster.Cluster.Name,
		ClusterArn:             *cluster.Cluster.Arn,
		Name:                   e2e.Truncate(stackName, 60),
		IAMRolesAnywhereCACert: rolesAnywhereCA.CertPEM,
		CFN:                    cfnClient,
		IAM:                    iamClient,
		EKS:                    eksClient,
	}
	stackOut, err := stack.Deploy(ctx, logger)
	if err != nil {
		return nil, err
	}

	return &Infrastructure{
		StackOutput:     *stackOut,
		RolesAnywhereCA: rolesAnywhereCA,
		stack:           stack,
		logger:          logger,
	}, nil
}

func (p *Infrastructure) Teardown(ctx context.Context) error {
	p.logger.Info("Deleting e2e resources stack", "stackName", p.stack.Name)
	return p.stack.Delete(ctx, p.logger, &p.StackOutput)
}
