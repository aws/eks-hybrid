package node

import (
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/configprovider"
	"github.com/aws/eks-hybrid/internal/node/ec2"
	"github.com/aws/eks-hybrid/internal/node/hybrid"
	"github.com/aws/eks-hybrid/internal/nodeprovider"
)

func NewNodeProvider(configSource string, skipPhases []string, logger *zap.Logger) (nodeprovider.NodeProvider, error) {
	logger.Info("Loading configuration..", zap.String("configSource", configSource))
	provider, err := configprovider.BuildConfigProvider(configSource)
	if err != nil {
		return nil, err
	}
	nodeConfig, err := provider.Provide()
	if err != nil {
		return nil, err
	}
	if nodeConfig.IsHybridNode() {
		logger.Info("Setting up hybrid node provider...")
		return hybrid.NewHybridNodeProvider(nodeConfig, skipPhases, logger)
	}
	logger.Info("Setting up EC2 node provider...")
	return ec2.NewEc2NodeProvider(nodeConfig, logger)
}
