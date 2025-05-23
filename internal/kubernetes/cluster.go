package kubernetes

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/aws/eks"
	"github.com/aws/eks-hybrid/internal/validation"
)

type ClusterProvider interface {
	ReadClusterDetails(ctx context.Context, node *api.NodeConfig, informer validation.Informer) (*api.ClusterDetails, error)
}

type clusterProvider struct {
	aws   aws.Config
	cache *api.ClusterDetails
}

func NewClusterProvider(config aws.Config) ClusterProvider {
	return &clusterProvider{
		aws: config,
	}
}

// ReadClusterDetails returns ClusterDetails with caching, delegating to eks.ReadClusterDetails for the actual API call
func (p *clusterProvider) ReadClusterDetails(ctx context.Context, node *api.NodeConfig, informer validation.Informer) (*api.ClusterDetails, error) {
	if node.Spec.Cluster.APIServerEndpoint != "" && node.Spec.Cluster.CertificateAuthority != nil && node.Spec.Cluster.CIDR != "" {
		return node.Spec.Cluster.DeepCopy(), nil
	}

	if p.cache != nil {
		return p.cache.DeepCopy(), nil
	}

	cluster, err := eks.ReadClusterDetails(ctx, p.aws, node)
	if err != nil {
		err = validation.WithRemediation(err,
			"Either provide the Kubernetes API server endpoint or ensure the node has access and permissions to call DescribeCluster EKS API.",
		)

		// Only if reading the EKS fail is when we "start" a validation and signal it as failed.
		// Otherwise, there is no need to surface we are reading from the EKS API.
		informer.Starting(ctx, "kubernetes-endpoint-access", "Validating access to Kubernetes API endpoint")
		informer.Done(ctx, "kubernetes-endpoint-access", err)

		return nil, err
	}

	p.cache = cluster.DeepCopy()
	return cluster, nil
}
