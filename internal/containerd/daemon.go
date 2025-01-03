package containerd

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/daemon"
)

const (
	ContainerdDaemonName     = "containerd"
	kernelModulesSystemdUnit = "systemd-modules-load"
)

var _ daemon.Daemon = &containerd{}

type containerd struct {
	daemonManager daemon.DaemonManager
	nodeConfig    *api.NodeConfig
	awsConfig     *aws.Config
	logger        *zap.Logger
}

func NewContainerdDaemon(daemonManager daemon.DaemonManager, cfg *api.NodeConfig, awsConfig *aws.Config, logger *zap.Logger) daemon.Daemon {
	return &containerd{
		daemonManager: daemonManager,
		nodeConfig:    cfg,
		awsConfig:     awsConfig,
		logger:        logger,
	}
}

func (cd *containerd) Configure() error {
	if err := writeContainerdConfig(cd.nodeConfig); err != nil {
		return err
	}
	return writeContainerdKernelModulesConfig()
}

// EnsureRunning ensures containerd is running with the written configuration
// With some installations, containerd daemon is already in an running state
// This enables the daemon and restarts or starts depending on the state of daemon
func (cd *containerd) EnsureRunning(ctx context.Context) error {
	if err := cd.daemonManager.RestartDaemon(ctx, kernelModulesSystemdUnit); err != nil {
		return err
	}

	if err := cd.daemonManager.EnableDaemon(ContainerdDaemonName); err != nil {
		return err
	}

	if err := cd.daemonManager.RestartDaemon(ctx, ContainerdDaemonName); err != nil {
		return err
	}

	runningCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	cd.logger.Info("Waiting for containerd to be running...")
	if err := daemon.WaitForStatus(runningCtx, cd.logger, cd.daemonManager, ContainerdDaemonName, daemon.DaemonStatusRunning, 5*time.Second); err != nil {
		return fmt.Errorf("waiting for containerd to be running: %w", err)
	}
	cd.logger.Info("containerd is running")

	return nil
}

func (cd *containerd) PostLaunch() error {
	return cacheSandboxImage(cd.awsConfig)
}

func (cd *containerd) Stop() error {
	return cd.daemonManager.StopDaemon(ContainerdDaemonName)
}

func (cd *containerd) Name() string {
	return ContainerdDaemonName
}
