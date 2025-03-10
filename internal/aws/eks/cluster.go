package eks

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"

	"github.com/aws/eks-hybrid/internal/api"
)

// ReadClusterDetails returns ClusterDetails with the API server endpoint, certificate authority, and CIDR block.
// If any of these are not set in the input node config, it retrieves them from the EKS API.
func ReadClusterDetails(ctx context.Context, config aws.Config, node *api.NodeConfig) (*api.ClusterDetails, error) {
	if node.Spec.Cluster.APIServerEndpoint != "" && node.Spec.Cluster.CertificateAuthority != nil && node.Spec.Cluster.CIDR != "" {
		return node.Spec.Cluster.DeepCopy(), nil
	}

	client := eks.NewFromConfig(config)
	input := &eks.DescribeClusterInput{
		Name: &node.Spec.Cluster.Name,
	}
	cluster, err := client.DescribeCluster(ctx, input)
	if err != nil {
		return nil, err
	}

	if cluster.Cluster.Status != types.ClusterStatusActive {
		return nil, fmt.Errorf("eks cluster %s is not active", *cluster.Cluster.Name)
	}

	clusterDetails := node.Spec.Cluster.DeepCopy()
	if clusterDetails.APIServerEndpoint == "" {
		clusterDetails.APIServerEndpoint = *cluster.Cluster.Endpoint
	}

	if clusterDetails.CertificateAuthority == nil {
		// CertificateAuthority from describeCluster api call returns base64 encoded data as a string
		// Decoding the string to byte array ensures the proper data format when writing to file
		decoded, err := base64.StdEncoding.DecodeString(*cluster.Cluster.CertificateAuthority.Data)
		if err != nil {
			return nil, err
		}
		clusterDetails.CertificateAuthority = decoded
	}

	if clusterDetails.CIDR == "" {
		clusterDetails.CIDR = *cluster.Cluster.KubernetesNetworkConfig.ServiceIpv4Cidr
	}

	return clusterDetails, nil
}
