package peered

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/go-logr/logr"

	"github.com/aws/eks-hybrid/test/e2e/credentials"
)

// Infrastructure represents the necessary infrastructure for peered VPCs to be used by nodeadm.
type Infrastructure struct {
	Credentials       credentials.Infrastructure
	Jumpbox           Jumpbox
	NodesPublicSSHKey string
}
type Jumpbox struct {
	Id string
	IP string
}

// Setup creates the necessary infrastructure for credentials providers to be used by nodeadm.
func Setup(ctx context.Context, logger logr.Logger, config aws.Config, clusterName, endpoint string) (*Infrastructure, error) {
	credsInfra, err := credentials.Setup(ctx, logger, config, clusterName, endpoint)
	if err != nil {
		return nil, err
	}

	ec2Client := ec2.NewFromConfig(config)

	jumpbox, err := JumpboxInstance(ctx, ec2Client, clusterName)
	if err != nil {
		return nil, err
	}

	keypair, err := KeyPair(ctx, ec2Client, clusterName)
	if err != nil {
		return nil, err
	}

	return &Infrastructure{
		Credentials: *credsInfra,
		Jumpbox: Jumpbox{
			Id: *jumpbox.InstanceId,
			IP: *jumpbox.PrivateIpAddress,
		},
		NodesPublicSSHKey: *keypair.PublicKey,
	}, nil
}

func (p *Infrastructure) Teardown(ctx context.Context) error {
	return p.Credentials.Teardown(ctx)
}
