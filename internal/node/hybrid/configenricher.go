package hybrid

import (
	"context"
	"encoding/base64"

	"github.com/aws/aws-sdk-go-v2/aws"
	eks_sdk "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/aws/ecr"
	"github.com/aws/eks-hybrid/internal/aws/eks"
)

func (p *HybridNodeProvider) Enrich(ctx context.Context) error {
	p.logger.Info("Enriching configuration...")
	eksRegistry, err := ecr.GetEKSHybridRegistry(p.nodeConfig.Spec.Cluster.Region)
	if err != nil {
		return err
	}
	p.nodeConfig.Status.Defaults.SandboxImage = eksRegistry.GetSandboxImage()

	p.logger.Info("Default options populated", zap.Reflect("defaults", p.nodeConfig.Status.Defaults))

	if needsClusterDetails(p.nodeConfig) {
		if err := ensureClusterDetails(ctx, p); err != nil {
			return err
		}

		p.logger.Info("Cluster details populated", zap.Reflect("cluster", p.nodeConfig.Spec.Cluster))
	}

	return nil
}

// ReadCluster calls eks.DescribeCluster and returns the cluster after ensuring it has a RemoteNetworkConfig
func ReadCluster(ctx context.Context, awsConfig aws.Config, nodeConfig *api.NodeConfig) (*eks.Cluster, error) {
	cluster, err := eks.DescribeCluster(ctx, eks.NewClient(awsConfig), nodeConfig.Spec.Cluster.Name)
	if err != nil {
		return nil, err
	}

	if cluster.Cluster.RemoteNetworkConfig == nil {
		return nil, errors.New("eks cluster does not have remoteNetworkConfig enabled, which is required for Hybrid Nodes")
	}

	return cluster.Cluster, nil
}

func needsClusterDetails(nodeConfig *api.NodeConfig) bool {
	return nodeConfig.Spec.Cluster.APIServerEndpoint == "" || nodeConfig.Spec.Cluster.CertificateAuthority == nil || nodeConfig.Spec.Cluster.CIDR == ""
}

func ensureClusterDetails(ctx context.Context, p *HybridNodeProvider) error {
	cluster, err := ReadCluster(ctx, *p.awsConfig, p.nodeConfig)
	if err != nil {
		return err
	}

	if cluster.Status != eks_sdk.ClusterStatusActive {
		return errors.New("eks cluster is not active")
	}

	p.remoteNetworkConfig = cluster.RemoteNetworkConfig

	if p.nodeConfig.Spec.Cluster.APIServerEndpoint == "" {
		p.nodeConfig.Spec.Cluster.APIServerEndpoint = *cluster.Endpoint
	}

	if p.nodeConfig.Spec.Cluster.CertificateAuthority == nil {
		// CertificateAuthority from describeCluster api call returns base64 encoded data as a string
		// Decoding the string to byte array ensures the proper data format when writing to file
		decoded, err := base64.StdEncoding.DecodeString(*cluster.CertificateAuthority.Data)
		if err != nil {
			return err
		}
		p.nodeConfig.Spec.Cluster.CertificateAuthority = decoded
	}

	if p.nodeConfig.Spec.Cluster.CIDR == "" {
		p.nodeConfig.Spec.Cluster.CIDR = *cluster.KubernetesNetworkConfig.ServiceIpv4Cidr
	}

	return nil
}
