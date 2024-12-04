package hybrid

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/nodeprovider"
)

type HybridNodeProvider struct {
	nodeConfig    *api.NodeConfig
	validator     func(config *api.NodeConfig) error
	awsConfig     *aws.Config
	daemonManager daemon.DaemonManager
	logger        *zap.Logger
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

func (hnp *HybridNodeProvider) GetNodeConfig() *api.NodeConfig {
	return hnp.nodeConfig
}

func (hnp *HybridNodeProvider) Logger() *zap.Logger {
	return hnp.logger
}

func (hnp *HybridNodeProvider) Cleanup() error {
	hnp.daemonManager.Close()
	return nil
}
