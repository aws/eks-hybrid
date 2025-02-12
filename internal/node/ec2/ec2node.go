package ec2

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/nodeprovider"
)

type ec2NodeProvider struct {
	nodeConfig    *api.NodeConfig
	awsConfig     *aws.Config
	daemonManager daemon.DaemonManager
	logger        *zap.Logger
	validator     func(config *api.NodeConfig) error
}

func NewEc2NodeProvider(nodeConfig *api.NodeConfig, logger *zap.Logger) (nodeprovider.NodeProvider, error) {
	np := &ec2NodeProvider{
		nodeConfig: nodeConfig,
		logger:     logger,
	}
	np.withEc2NodeValidators()
	if err := np.withDaemonManager(); err != nil {
		return nil, err
	}
	return np, nil
}

func (enp *ec2NodeProvider) GetNodeConfig() *api.NodeConfig {
	return enp.nodeConfig
}

func (enp *ec2NodeProvider) Logger() *zap.Logger {
	return enp.logger
}

func (enp *ec2NodeProvider) ValidateNodeIP(ctx context.Context) error { return nil }

func (enp *ec2NodeProvider) Cleanup() error {
	enp.daemonManager.Close()
	return nil
}
