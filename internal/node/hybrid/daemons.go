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
	credentialProviderAwsConfig := kubelet.CredentialProviderAwsConfig{}
	if hnp.nodeConfig.IsIAMRolesAnywhere() {
		credentialProviderAwsConfig.Profile = iamrolesanywhere.ProfileName
		credentialProviderAwsConfig.CredentialsPath = iamrolesanywhere.EksHybridAwsCredentialsPath
	}
	return []daemon.Daemon{
		containerd.NewContainerdDaemon(hnp.daemonManager, hnp.nodeConfig, hnp.awsConfig, hnp.logger),
		kubelet.NewKubeletDaemon(hnp.daemonManager, hnp.nodeConfig, hnp.awsConfig, credentialProviderAwsConfig, hnp.logger, hnp.skipPhases),
	}, nil
}

func (hnp *HybridNodeProvider) PreProcessDaemon(ctx context.Context) error {
	return nil
}
