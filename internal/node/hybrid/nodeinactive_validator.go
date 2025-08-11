package hybrid

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/validation"
)

const (
	kubeletEnvironmentFilePath = "/etc/eks/kubelet/environment"
	etcKubernetesDir           = "/etc/kubernetes"
	nodeActiveRemediation      = "Ensure the hybrid node is made inactive by running 'nodeadm uninstall' before attaching to the EKS cluster"
)

// ValidateNodeIsInactive checks to see if the node is potentially active.
func (hnp *HybridNodeProvider) ValidateNodeIsInactive(ctx context.Context, informer validation.Informer, _ *api.NodeConfig) error {
	var err error
	informer.Starting(ctx, nodeInactiveValidation, "Validating that the node is inactive")
	defer func() {
		informer.Done(ctx, nodeInactiveValidation, err)
	}()

	if _, statErr := os.Stat(kubeletEnvironmentFilePath); !os.IsNotExist(statErr) {
		if statErr == nil {
			err = validation.WithWarning(fmt.Errorf("kubelet args environment file %s exists", kubeletEnvironmentFilePath), nodeActiveRemediation)
			return err
		}
		err = validation.WithWarning(fmt.Errorf("checking kubelet environment file %s cleanup: %w", kubeletEnvironmentFilePath, statErr), nodeActiveRemediation)
		return err
	}

	if _, statErr := os.Stat(etcKubernetesDir); !os.IsNotExist(statErr) {
		if statErr == nil {
			err = validation.WithWarning(fmt.Errorf("kubernetes directory %s still exists", etcKubernetesDir), nodeActiveRemediation)
			return err
		}
		err = validation.WithWarning(fmt.Errorf("checking kubernetes directory %s cleanup: %w", etcKubernetesDir, statErr), nodeActiveRemediation)
		return err
	}

	kubeletStatus, daemonErr := hnp.daemonManager.GetDaemonStatus(kubelet.KubeletDaemonName)
	if daemonErr != nil {
		err = validation.WithWarning(daemonErr, nodeActiveRemediation)
		return err
	}

	if kubeletStatus == daemon.DaemonStatusRunning {
		err = validation.WithWarning(fmt.Errorf("kubelet service is still active and may be connected to a previous cluster"), nodeActiveRemediation)
		return err
	}

	return nil
}
