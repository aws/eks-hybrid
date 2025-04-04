package cluster

import (
	"context"
	"encoding/base64"
	stdErr "errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/errors"
)

const (
	createClusterTimeout = 40 * time.Minute
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

func (h *hybridCluster) create(ctx context.Context, client *eks.Client, logger logr.Logger) (*types.Cluster, error) {
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
		Logging: &types.Logging{
			ClusterLogging: []types.LogSetup{
				{
					Enabled: aws.Bool(true),
					Types:   []types.LogType{"api", "audit", "authenticator", "controllerManager", "scheduler"},
				},
			},
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
	_, err := client.CreateCluster(ctx, hybridCluster)
	if err != nil && !errors.IsType(err, &types.ResourceInUseException{}) {
		return nil, fmt.Errorf("creating EKS hybrid cluster: %w", err)
	}

	logger.Info("Waiting for cluster to be active", "cluster", h.Name)
	cluster, err := waitForActiveCluster(ctx, client, h.Name)
	if err != nil {
		if cluster != nil {
			logger.Error(err, "Failed waiting for cluster to be active", "cluster", cluster)
		}
		return nil, err
	}

	logger.Info("Successfully started EKS hybrid cluster", "cluster", cluster)

	return cluster, nil
}

// waitForActiveCluster waits until the cluster is in the 'ACTIVE' state.
func waitForActiveCluster(ctx context.Context, client *eks.Client, clusterName string) (*types.Cluster, error) {
	ctx, cancel := context.WithTimeout(ctx, createClusterTimeout)
	defer cancel()
	var cluster *types.Cluster
	err := waitForCluster(ctx, client, clusterName, func(output *eks.DescribeClusterOutput, err error) (bool, error) {
		if err != nil {
			return false, fmt.Errorf("describing cluster %s: %w", clusterName, err)
		}
		cluster = output.Cluster

		switch output.Cluster.Status {
		case types.ClusterStatusActive:
			return true, nil
		case types.ClusterStatusFailed:
			return false, fmt.Errorf("cluster %s creation failed", clusterName)
		case types.ClusterStatusDeleting:
			return false, fmt.Errorf("cluster %s is being deleted", clusterName)
		default:
			return false, nil
		}
	})
	return cluster, err
}

func (h *hybridCluster) UpdateKubeconfig(cluster *types.Cluster, kubeconfig string) error {
	// data is already base64 encoded from the API
	// when the kubeconfig is written out it will be base64 encoded
	caPEMData, err := base64.StdEncoding.DecodeString(*cluster.CertificateAuthority.Data)
	if err != nil {
		return fmt.Errorf("decoding certificate authority data: %w", err)
	}

	clientConfig := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			*cluster.Arn: {
				Server:                   *cluster.Endpoint,
				CertificateAuthorityData: caPEMData,
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			*cluster.Arn: {
				Cluster:  *cluster.Arn,
				AuthInfo: *cluster.Arn,
			},
		},
		CurrentContext: *cluster.Arn,
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			*cluster.Arn: {
				Exec: &clientcmdapi.ExecConfig{
					APIVersion: "client.authentication.k8s.io/v1beta1",
				},
			},
		},
	}

	awsProfile := os.Getenv("AWS_PROFILE")
	if awsProfile == "" {
		awsProfile = os.Getenv("AWS_DEFAULT_PROFILE")
	}
	if awsProfile != "" {
		clientConfig.AuthInfos[*cluster.Arn].Exec.Env = []clientcmdapi.ExecEnvVar{
			{Name: "AWS_PROFILE", Value: awsProfile},
		}
	}

	// in the canaries the aws cli may not be installed, but aws-iam-authenticator will be available
	// fall back to aws-iam-authenticator if aws cli is not found
	_, awsCliErr := exec.LookPath("aws")
	_, iamAuthErr := exec.LookPath("aws-iam-authenticator")
	if awsCliErr != nil && iamAuthErr != nil {
		return fmt.Errorf("neither aws cli nor aws-iam-authenticator found in PATH: %w", stdErr.Join(awsCliErr, iamAuthErr))
	}

	if awsCliErr == nil {
		clientConfig.AuthInfos[*cluster.Arn].Exec.Command = "aws"
		clientConfig.AuthInfos[*cluster.Arn].Exec.Args = []string{"eks", "get-token", "--cluster-name", h.Name, "--output", "json"}
	} else {
		clientConfig.AuthInfos[*cluster.Arn].Exec.Command = "aws-iam-authenticator"
		clientConfig.AuthInfos[*cluster.Arn].Exec.Args = []string{"token", "-i", h.Name}
	}

	if err := clientcmd.WriteToFile(clientConfig, kubeconfig); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}
	return nil
}

func waitForCluster(ctx context.Context, client *eks.Client, clusterName string, check func(*eks.DescribeClusterOutput, error) (bool, error)) error {
	statusCh := make(chan bool)
	errCh := make(chan error)

	retries := 0
	go func(ctx context.Context) {
		defer close(statusCh)
		defer close(errCh)
		for {
			describeInput := &eks.DescribeClusterInput{
				Name: aws.String(clusterName),
			}
			done, err := check(client.DescribeCluster(ctx, describeInput))
			if err != nil {
				errCh <- err
				return
			}
			retries++

			if done {
				return
			}

			select {
			case <-ctx.Done(): // Check if the context is done (timeout/canceled)
				errCh <- fmt.Errorf("context canceled or timed out while waiting for cluster %s after %d retries: %v", clusterName, retries, ctx.Err())
				return
			case <-time.After(30 * time.Second):
			}
		}
	}(ctx)

	// Wait for the cluster to be deleted or for the timeout to expire
	select {
	case <-statusCh:
		return nil
	case err := <-errCh:
		return err
	}
}
