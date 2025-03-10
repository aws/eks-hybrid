package hybrid

import (
	"context"
	"encoding/base64"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
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
		if err := ensureClusterDetails(ctx, *p.awsConfig, p.nodeConfig); err != nil {
			return err
		}

		p.logger.Info("Cluster details populated", zap.Reflect("cluster", p.nodeConfig.Spec.Cluster))
	}

	return nil
}

func needsClusterDetails(nodeConfig *api.NodeConfig) bool {
	return nodeConfig.Spec.Cluster.APIServerEndpoint == "" || nodeConfig.Spec.Cluster.CertificateAuthority == nil || nodeConfig.Spec.Cluster.CIDR == ""
}

func readCluster(ctx context.Context, awsConfig aws.Config, nodeConfig *api.NodeConfig) (*types.Cluster, error) {
	client := eks.NewClient(awsConfig)
	cluster, err := eks.DescribeCluster(ctx, client, nodeConfig.Spec.Cluster.Name)
	if err != nil {
		return nil, err
	}

	return cluster.Cluster, nil
}

func ensureClusterDetails(ctx context.Context, awsConfig aws.Config, nodeConfig *api.NodeConfig) error {
	cluster, err := readCluster(ctx, awsConfig, nodeConfig)
	if err != nil {
		return err
	}

	if cluster.Status != types.ClusterStatusActive {
		return errors.New("eks cluster is not active")
	}

	if cluster.RemoteNetworkConfig == nil {
		return errors.New("eks cluster does not have remoteNetworkConfig enabled, which is required for Hybrid Nodes")
	}

	if nodeConfig.Spec.Cluster.APIServerEndpoint == "" {
		nodeConfig.Spec.Cluster.APIServerEndpoint = *cluster.Endpoint
	}

	if nodeConfig.Spec.Cluster.CertificateAuthority == nil {
		// CertificateAuthority from describeCluster api call returns base64 encoded data as a string
		// Decoding the string to byte array ensures the proper data format when writing to file
		decoded, err := base64.StdEncoding.DecodeString(*cluster.CertificateAuthority.Data)
		if err != nil {
			return err
		}
		nodeConfig.Spec.Cluster.CertificateAuthority = decoded
	}

	if nodeConfig.Spec.Cluster.CIDR == "" {
		nodeConfig.Spec.Cluster.CIDR = *cluster.KubernetesNetworkConfig.ServiceIpv4Cidr
	}

	return nil
}
