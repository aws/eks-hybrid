package system

import (
	"os"
	"os/exec"
	"strings"

	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
)

const localDiskAspectName = "local-disk"

func NewLocalDiskAspect(cfg *api.NodeConfig) SystemAspect {
	return &localDiskAspect{nodeConfig: cfg}
}

type localDiskAspect struct {
	nodeConfig *api.NodeConfig
}

func (a *localDiskAspect) Name() string {
	return localDiskAspectName
}

func (a *localDiskAspect) Setup() error {
	if a.nodeConfig.Spec.Instance.LocalStorage.Strategy == "" {
		zap.L().Info("Not configuring local disks!")
		return nil
	}
	strategy := strings.ToLower(string(a.nodeConfig.Spec.Instance.LocalStorage.Strategy))
	// #nosec G204 Subprocess launched with variable
	cmd := exec.Command("setup-local-disks", strategy)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
