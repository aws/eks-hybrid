package peered

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/credentials"
)

// Infrastructure represents the necessary infrastructure for peered VPCs to be used by nodeadm.
type Infrastructure struct {
	Credentials       credentials.Infrastructure
	JumpboxInstanceId string
	NodesPublicSSHKey string
}

// Setup creates the necessary infrastructure for credentials providers to be used by nodeadm.
func Setup(ctx context.Context, logger logr.Logger, awsSession *session.Session, config aws.Config, clusterName string) (*Infrastructure, error) {
	credsInfra, err := credentials.Setup(ctx, logger, awsSession, config, clusterName)
	if err != nil {
		return nil, err
	}

	ec2Client := ec2.NewFromConfig(config)

	instances, err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("tag:" + constants.TestClusterTagKey),
				Values: []string{clusterName},
			},
			{
				Name:   aws.String("tag:Jumpbox"),
				Values: []string{"true"},
			},
			{
				Name:   aws.String("instance-state-name"),
				Values: []string{"running"},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	if len(instances.Reservations) == 0 || len(instances.Reservations[0].Instances) == 0 {
		return nil, fmt.Errorf("no jumpbox instance found for cluster %s", clusterName)
	}

	keypair, err := ec2Client.DescribeKeyPairs(ctx, &ec2.DescribeKeyPairsInput{
		IncludePublicKey: aws.Bool(true),
		Filters: []types.Filter{
			{
				Name:   aws.String("tag:" + constants.TestClusterTagKey),
				Values: []string{clusterName},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	if len(keypair.KeyPairs) == 0 {
		return nil, fmt.Errorf("no key pair found for cluster %s", clusterName)
	}

	return &Infrastructure{
		Credentials:       *credsInfra,
		JumpboxInstanceId: *instances.Reservations[0].Instances[0].InstanceId,
		NodesPublicSSHKey: *keypair.KeyPairs[0].PublicKey,
	}, nil
}

func (p *Infrastructure) Teardown(ctx context.Context) error {
	return p.Credentials.Teardown(ctx)
}
