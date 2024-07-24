package ssm

import (
	"os"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/util"
)

var (
	_             daemon.Daemon = &ssm{}
	SsmDaemonName               = "amazon-ssm-agent"
)

type ssm struct {
	daemonManager daemon.DaemonManager
}

func NewSsmDaemon(daemonManager daemon.DaemonManager) daemon.Daemon {
	setDaemonName()
	return &ssm{
		daemonManager: daemonManager,
	}
}

func (s *ssm) Configure(cfg *api.NodeConfig) error {
	_, err := GetManagedHybridInstanceId()
	if err != nil && os.IsNotExist(err) {
		// The node is not registered with SSM
		// In some cases, while the node is not registered, there might be some leftover
		// registration data from previous registrations. Setting force to true, will override
		// leftover registration data from the service cache.
		return s.registerMachine(cfg, true)
	} else if err != nil {
		return err
	}
	return s.registerMachine(cfg, false)
}

func (s *ssm) EnsureRunning() error {
	err := s.daemonManager.EnableDaemon(SsmDaemonName)
	if err != nil {
		return err
	}
	return s.daemonManager.StartDaemon(SsmDaemonName)
}

func (s *ssm) PostLaunch(_ *api.NodeConfig) error {
	return nil
}

// Stop stops the ssm unit only if it is loaded and running
func (s *ssm) Stop() error {
	status, err := s.daemonManager.GetDaemonStatus(SsmDaemonName)
	if err != nil {
		return err
	}
	if status == daemon.DaemonStatusRunning {
		return s.daemonManager.StopDaemon(SsmDaemonName)
	}
	return nil
}

func (s *ssm) Name() string {
	return SsmDaemonName
}

func setDaemonName() {
	osToDaemonName := map[string]string{
		util.UbuntuOsName: "snap.amazon-ssm-agent.amazon-ssm-agent",
		util.RhelOsName:   "amazon-ssm-agent",
		util.AmazonOsName: "amazon-ssm-agent",
	}
	osName := util.GetOsName()
	if daemonName, ok := osToDaemonName[osName]; ok {
		SsmDaemonName = daemonName
	}
}