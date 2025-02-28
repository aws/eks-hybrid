package hybrid

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"go.uber.org/zap"
	"k8s.io/utils/strings/slices"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/aws/eks"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/nodeprovider"
)

const ipValidation = "ip-validation"

type HybridNodeProvider struct {
	nodeConfig    *api.NodeConfig
	validator     func(config *api.NodeConfig) error
	awsConfig     *aws.Config
	daemonManager daemon.DaemonManager
	logger        *zap.Logger
	cluster       *eks.Cluster
}

type NodeProviderOpt func(*HybridNodeProvider)

func NewHybridNodeProvider(nodeConfig *api.NodeConfig, logger *zap.Logger, opts ...NodeProviderOpt) (nodeprovider.NodeProvider, error) {
	np := &HybridNodeProvider{
		nodeConfig: nodeConfig,
		logger:     logger,
	}
	np.withHybridValidators()
	if err := np.withDaemonManager(); err != nil {
		return nil, err
	}

	for _, opt := range opts {
		opt(np)
	}

	return np, nil
}

func WithAWSConfig(config *aws.Config) NodeProviderOpt {
	return func(hnp *HybridNodeProvider) {
		hnp.awsConfig = config
	}
}

func WithCluster(cluster *eks.Cluster) NodeProviderOpt {
	return func(hnp *HybridNodeProvider) {
		hnp.cluster = cluster
	}
}

func (hnp *HybridNodeProvider) GetNodeConfig() *api.NodeConfig {
	return hnp.nodeConfig
}

func (hnp *HybridNodeProvider) Logger() *zap.Logger {
	return hnp.logger
}

func (hnp *HybridNodeProvider) Validate(ctx context.Context, skipPhases []string) error {
	if !slices.Contains(skipPhases, ipValidation) {
		if hnp.cluster == nil {
			hnp.Logger().Info("Warning: EKS Cluster details not retrieved - IP validation skipped")
		} else {
			hnp.logger.Info("Validating Node IP...")

			// Only check flags set by user since hybrid nodes do not set --node-ip flag
			// and we want to prevent hostname-override by user
			kubeletArgs := hnp.nodeConfig.Spec.Kubelet.Flags
			var iamNodeName string
			if hnp.nodeConfig.IsIAMRolesAnywhere() {
				iamNodeName = hnp.nodeConfig.Status.Hybrid.NodeName
			}
			nodeIp, err := getNodeIP(kubeletArgs, iamNodeName)
			if err != nil {
				return err
			}

			cluster, err := hnp.Cluster(ctx)
			if err != nil {
				return err
			}
			if validateClusterRemoteNetworkConfig(cluster) != nil {
				return err
			}

			if err = validateIPInRemoteNodeNetwork(nodeIp, cluster.RemoteNetworkConfig.RemoteNodeNetworks); err != nil {
				return err
			}
		}
	}

	return nil
}

func (hnp *HybridNodeProvider) Cleanup() error {
	hnp.daemonManager.Close()
	return nil
}

// Cluster retrieves the eks.Cluster object or makes a DescribeCluster call to the EKS API and caches the result if not already present
func (p *HybridNodeProvider) Cluster(ctx context.Context) (*eks.Cluster, error) {
	if p.cluster != nil {
		return p.cluster, nil
	}

	cluster, err := readCluster(ctx, *p.awsConfig, p.nodeConfig)
	if err != nil {
		p.logger.Error("Failed to read cluster", zap.Error(err))
		return nil, err
	}
	p.cluster = cluster

	return cluster, nil
}
