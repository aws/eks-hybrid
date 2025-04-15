package hybrid

import (
	"context"
	"encoding/base64"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/aws/ecr"
	"github.com/aws/eks-hybrid/internal/configenricher"
	"github.com/aws/eks-hybrid/internal/kubernetes"
	"github.com/aws/eks-hybrid/internal/validation"
)

func (hnp *HybridNodeProvider) Enrich(ctx context.Context, opts ...configenricher.ConfigEnricherOption) error {
	hnp.logger.Info("Enriching configuration...")

	// Apply options to build configuration
	config := &configenricher.ConfigEnricherConfig{}
	for _, opt := range opts {
		opt(config)
	}

	eksRegistry, err := ecr.GetEKSHybridRegistry(hnp.nodeConfig.Spec.Cluster.Region, config.RegionConfig)
	if err != nil {
		return err
	}
	hnp.nodeConfig.Status.Defaults.SandboxImage = eksRegistry.GetSandboxImage()

	hnp.logger.Info("Default options populated", zap.Reflect("defaults", hnp.nodeConfig.Status.Defaults))

	if needsClusterDetails(hnp.nodeConfig) {
		if err := hnp.ensureClusterDetails(ctx); err != nil {
			return err
		}

		hnp.logger.Info("Cluster details populated", zap.Reflect("cluster", hnp.nodeConfig.Spec.Cluster))
	}

	// Validate access to the Kubernetes API endpoint
	if err := hnp.runner.Run(ctx, hnp.nodeConfig,
		validation.New("k8s-endpoint-network", kubernetes.NewAccessValidator(*hnp.awsConfig).Run),
	); err != nil {
		return err
	}

	return nil
}

// readCluster calls eks.DescribeCluster and returns the cluster
func readCluster(ctx context.Context, awsConfig aws.Config, nodeConfig *api.NodeConfig) (*types.Cluster, error) {
	client := eks.NewFromConfig(awsConfig)
	input := &eks.DescribeClusterInput{
		Name: &nodeConfig.Spec.Cluster.Name,
	}
	cluster, err := client.DescribeCluster(ctx, input)
	if err != nil {
		return nil, err
	}

	return cluster.Cluster, nil
}

func needsClusterDetails(nodeConfig *api.NodeConfig) bool {
	return nodeConfig.Spec.Cluster.APIServerEndpoint == "" || nodeConfig.Spec.Cluster.CertificateAuthority == nil || nodeConfig.Spec.Cluster.CIDR == ""
}

func (hnp *HybridNodeProvider) ensureClusterDetails(ctx context.Context) error {
	cluster, err := hnp.getCluster(ctx)
	if err != nil {
		return err
	}

	if cluster.Status != types.ClusterStatusActive {
		return errors.New("eks cluster is not active")
	}

	if cluster.RemoteNetworkConfig == nil {
		return errors.New("eks cluster does not have remoteNetworkConfig enabled, which is required for Hybrid Nodes")
	}

	if hnp.nodeConfig.Spec.Cluster.APIServerEndpoint == "" {
		hnp.nodeConfig.Spec.Cluster.APIServerEndpoint = *cluster.Endpoint
	}

	if hnp.nodeConfig.Spec.Cluster.CertificateAuthority == nil {
		// CertificateAuthority from describeCluster api call returns base64 encoded data as a string
		// Decoding the string to byte array ensures the proper data format when writing to file
		decoded, err := base64.StdEncoding.DecodeString(*cluster.CertificateAuthority.Data)
		if err != nil {
			return err
		}
		hnp.nodeConfig.Spec.Cluster.CertificateAuthority = decoded
	}

	if hnp.nodeConfig.Spec.Cluster.CIDR == "" {
		hnp.nodeConfig.Spec.Cluster.CIDR = *cluster.KubernetesNetworkConfig.ServiceIpv4Cidr
	}

	return nil
}
