package credentials

import (
	"context"
	"fmt"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	ssmv2 "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmv2Types "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ssm"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/creds"
	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/constants"
)

const ssmActivationName = "eks-hybrid-ssm-provider"

type SsmProvider struct {
	SSM   *ssm.SSM
	SSMv2 *ssmv2.Client
	Role  string
}

func (s *SsmProvider) Name() creds.CredentialProvider {
	return creds.SsmCredentialProvider
}

func (s *SsmProvider) InstanceID(node e2e.HybridEC2Node) string {
	return node.Node.Name
}

func (s *SsmProvider) NodeadmConfig(ctx context.Context, node e2e.NodeSpec) (*api.NodeConfig, error) {
	ssmActivationDetails, err := createSSMActivation(ctx, s.SSMv2, s.Role, ssmActivationName, node.Cluster.Name)
	if err != nil {
		return nil, err
	}
	return &api.NodeConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "node.eks.aws/v1alpha1",
			Kind:       "NodeConfig",
		},
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Name:   node.Cluster.Name,
				Region: node.Cluster.Region,
			},
			Hybrid: &api.HybridOptions{
				SSM: &api.SSM{
					ActivationID:   *ssmActivationDetails.ActivationId,
					ActivationCode: *ssmActivationDetails.ActivationCode,
				},
			},
		},
	}, nil
}

func (s *SsmProvider) VerifyUninstall(ctx context.Context, instanceId string) error {
	return waitForManagedInstanceUnregistered(ctx, s.SSM, instanceId)
}

func (s *SsmProvider) FilesForNode(_ e2e.NodeSpec) ([]e2e.File, error) {
	return nil, nil
}

func createSSMActivation(ctx context.Context, client *ssmv2.Client, iamRole, ssmActivationName, clusterName string) (*ssmv2.CreateActivationOutput, error) {
	// Define the input for the CreateActivation API
	input := &ssmv2.CreateActivationInput{
		IamRole:             aws.String(iamRole),
		RegistrationLimit:   aws.Int32(2),
		DefaultInstanceName: aws.String(ssmActivationName),
		Tags: []ssmv2Types.Tag{
			{
				Key:   aws.String(constants.TestClusterTagKey),
				Value: aws.String(clusterName),
			},
		},
	}

	// Call CreateActivation to create the SSM activation
	result, err := client.CreateActivation(ctx, input, func(o *ssmv2.Options) {
		o.RetryMaxAttempts = 20
		o.RetryMode = awsv2.RetryModeAdaptive
	})
	if err != nil {
		return nil, fmt.Errorf("creating SSM activation: %v", err)
	}

	return result, nil
}

func waitForManagedInstanceUnregistered(ctx context.Context, ssmClient *ssm.SSM, instanceId string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	statusCh := make(chan struct{})
	errCh := make(chan error)
	consecutiveErrors := 0

	go func() {
		defer close(statusCh)
		defer close(errCh)
		for {
			output, err := ssmClient.DescribeInstanceInformationWithContext(ctx, &ssm.DescribeInstanceInformationInput{
				Filters: []*ssm.InstanceInformationStringFilter{
					{
						Key:    aws.String("InstanceIds"),
						Values: []*string{aws.String(instanceId)},
					},
				},
			})
			if err != nil {
				consecutiveErrors += 1
				if consecutiveErrors > 3 || ctx.Err() != nil {
					errCh <- fmt.Errorf("failed to describe instance information %s: %v", instanceId, err)
					return
				}
			} else if len(output.InstanceInformationList) == 0 {
				statusCh <- struct{}{}
				return
			} else {
				consecutiveErrors = 0
			}

			time.Sleep(5 * time.Second)
		}
	}()

	select {
	case <-statusCh:
		return nil
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return fmt.Errorf("timed out waiting for instance to unregister: %s", instanceId)
	}
}
