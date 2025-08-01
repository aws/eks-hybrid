package hybrid

import (
	"fmt"
	"os"

	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/kubelet"
)

const (
	kubeletEnvironmentFilePath = "/etc/eks/kubelet/environment"
	etcKubernetesDir           = "/etc/kubernetes"
)

// ValidateNodeIsInactive checks to see if the node is potentially active.
func (hnp *HybridNodeProvider) ValidateNodeIsInactive() error {
	if _, err := os.Stat(kubeletEnvironmentFilePath); !os.IsNotExist(err) {
		if err == nil {
			return fmt.Errorf("kubelet args environment file %s exists", kubeletEnvironmentFilePath)
		}
		return fmt.Errorf("checking kubelet environment file %s cleanup: %w", kubeletEnvironmentFilePath, err)
	}

	if _, err := os.Stat(etcKubernetesDir); !os.IsNotExist(err) {
		if err == nil {
			return fmt.Errorf("kubernetes directory %s still exists", etcKubernetesDir)
		}
		return fmt.Errorf("checking kubernetes directory %s cleanup: %w", etcKubernetesDir, err)
	}

	kubeletStatus, err := hnp.daemonManager.GetDaemonStatus(kubelet.KubeletDaemonName)
	if err != nil {
		return err
	}

	if kubeletStatus == daemon.DaemonStatusRunning {
		return fmt.Errorf("kubelet service is still active, likely connected to old cluster")
	}

	return nil
}
