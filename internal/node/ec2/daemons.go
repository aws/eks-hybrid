package ec2

import (
	"context"

	"github.com/pkg/errors"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/containerd"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/validation"
)

func (enp *ec2NodeProvider) withDaemonManager() error {
	manager, err := daemon.NewDaemonManager()
	if err != nil {
		return err
	}
	enp.daemonManager = manager
	return nil
}

func (enp *ec2NodeProvider) GetDaemons() ([]daemon.Daemon, error) {
	if enp.awsConfig == nil {
		return nil, errors.New("aws config not set")
	}
	return []daemon.Daemon{
		containerd.NewContainerdDaemon(enp.daemonManager, enp.nodeConfig, enp.awsConfig, enp.logger),
		kubelet.NewKubeletDaemon(
			enp.daemonManager,
			enp.nodeConfig,
			enp.awsConfig,
			kubelet.CredentialProviderAwsConfig{},
			// For EC2 nodes we don't run in-flight validations
			// We could but we don't to preserve the current (now old) behavior
			validation.NewNoopSingleRunner[*api.NodeConfig](),
		),
	}, nil
}

func (enp *ec2NodeProvider) PreProcessDaemon(_ context.Context) error {
	return nil
}
