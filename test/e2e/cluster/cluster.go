package cluster

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awsutil"
	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/go-logr/logr"
)

const (
	createClusterTimeout = 15 * time.Minute
)

type hybridCluster struct {
	Name              string
	Region            string
	KubernetesVersion string
	SecurityGroup     string
	SubnetIDs         []string
	Role              string
	HybridNetwork     NetworkConfig
}

func (h *hybridCluster) create(ctx context.Context, client *eks.Client, logger logr.Logger) error {
	hybridCluster := &eks.CreateClusterInput{
		Name:    aws.String(h.Name),
		Version: aws.String(h.KubernetesVersion),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds:        h.SubnetIDs,
			SecurityGroupIds: []string{h.SecurityGroup},
		},
		RoleArn: aws.String(h.Role),
		Tags: map[string]string{
			constants.TestClusterTagKey: h.Name,
		},
		AccessConfig: &types.CreateAccessConfigRequest{
			AuthenticationMode: types.AuthenticationModeApiAndConfigMap,
		},
		RemoteNetworkConfig: &types.RemoteNetworkConfigRequest{
			RemoteNodeNetworks: []types.RemoteNodeNetwork{
				{
					Cidrs: []string{h.HybridNetwork.VpcCidr},
				},
			},
			RemotePodNetworks: []types.RemotePodNetwork{
				{
					Cidrs: []string{h.HybridNetwork.PodCidr},
				},
			},
		},
	}
	clusterOutput, err := client.CreateCluster(ctx, hybridCluster)
	if err != nil && !e2e.IsErrorType(err, &types.ResourceInUseException{}) {
		return fmt.Errorf("creating EKS hybrid cluster: %w", err)
	}

	if err := waitForClusterCreation(ctx, client, h.Name); err != nil {
		return err
	}

	if clusterOutput.Cluster != nil {
		logger.Info("Successfully started EKS hybrid cluster", "output", awsutil.Prettify(clusterOutput))
	}

	return nil
}

// waitForClusterCreation waits until the cluster is in the 'ACTIVE' state.
func waitForClusterCreation(ctx context.Context, client *eks.Client, clusterName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), createClusterTimeout)
	defer cancel()

	statusCh := make(chan bool)
	errCh := make(chan error)

	go func() {
		defer close(statusCh)
		defer close(errCh)
		for {
			describeInput := &eks.DescribeClusterInput{
				Name: aws.String(clusterName),
			}
			output, err := client.DescribeCluster(ctx, describeInput)
			if err != nil {
				errCh <- fmt.Errorf("failed to describe cluster %s: %v", clusterName, err)
				return
			}

			clusterStatus := output.Cluster.Status
			if clusterStatus == types.ClusterStatusActive {
				statusCh <- true
				return
			} else if clusterStatus == types.ClusterStatusFailed {
				errCh <- fmt.Errorf("cluster %s creation failed", clusterName)
				return
			}

			// Sleep for 30 secs before checking again
			time.Sleep(30 * time.Second)
		}
	}()

	// Wait for the cluster to become active or for the timeout to expire
	select {
	case <-statusCh:
		return nil
	case err := <-errCh:
		return err
	}
}

func (h *hybridCluster) UpdateKubeconfig(kubeconfig string) error {
	cmd := exec.Command("aws", "eks", "update-kubeconfig", "--name", h.Name, "--region", h.Region, "--kubeconfig", kubeconfig)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
