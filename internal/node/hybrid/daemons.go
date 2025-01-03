package hybrid

import (
	"context"

	"github.com/pkg/errors"

	"github.com/aws/eks-hybrid/internal/containerd"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/iamrolesanywhere"
	"github.com/aws/eks-hybrid/internal/kubelet"
)

func (hnp *HybridNodeProvider) withDaemonManager() error {
	manager, err := daemon.NewDaemonManager()
	if err != nil {
		return err
	}
	hnp.daemonManager = manager
	return nil
}

func (hnp *HybridNodeProvider) GetDaemons() ([]daemon.Daemon, error) {
	if hnp.awsConfig == nil {
		return nil, errors.New("aws config not set")
	}
	return []daemon.Daemon{
		containerd.NewContainerdDaemon(hnp.daemonManager, hnp.nodeConfig, hnp.awsConfig, hnp.logger),
		kubelet.NewKubeletDaemon(hnp.daemonManager, hnp.nodeConfig, hnp.awsConfig),
	}, nil
}

func (hnp *HybridNodeProvider) PreProcessDaemon(ctx context.Context) error {
	if hnp.nodeConfig.IsIAMRolesAnywhere() {
		if hnp.nodeConfig.Spec.Hybrid.EnableCredentialsFile {
			hnp.logger.Info("Configuring aws_signing_helper_update daemon")
			signingHelper := iamrolesanywhere.NewSigningHelperDaemon(hnp.daemonManager, hnp.nodeConfig)
			if err := signingHelper.Configure(); err != nil {
				return err
			}
			if err := signingHelper.EnsureRunning(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}
