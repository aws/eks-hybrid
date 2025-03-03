package system

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/firewall"
)

const (
	portsAspectName        = "ports"
	kubeletServePort       = "10250"
	kubeProxyHealthzPort   = "10256"
	nodePortStartRangePort = "30000"
	nodePortEndRangePort   = "32767"
)

type portsAspect struct {
	nodeConfig      *api.NodeConfig
	logger          *zap.Logger
	firewallManager firewall.Manager
}

var _ SystemAspect = &portsAspect{}

func NewPortsAspect(cfg *api.NodeConfig, logger *zap.Logger) SystemAspect {
	return &portsAspect{
		nodeConfig:      cfg,
		logger:          logger,
		firewallManager: NewFirewallManager(),
	}
}

func NewFirewallManager() firewall.Manager {
	osName := GetOsName()
	if osName == UbuntuOsName {
		return firewall.NewUncomplicatedFirewall()
	}
	return firewall.NewFirewalld()
}

func (s *portsAspect) Name() string {
	return portsAspectName
}

func (s *portsAspect) Setup() error {
	firewallEnabled, err := s.firewallManager.IsEnabled()
	if err != nil {
		s.logger.Warn("Failed to get firewall status", zap.Error(err))
		s.logger.Info("Skip setting firewall rules")
		return nil
	}
	if firewallEnabled {
		s.logger.Info("Allowing port on firewall", zap.Reflect("kubelet-server-port", kubeletServePort))
		if err = s.firewallManager.AllowTcpPort(kubeletServePort); err != nil {
			return err
		}
		s.logger.Info("Allowing port on firewall", zap.Reflect("kube-proxy-port", kubeProxyHealthzPort))
		if err = s.firewallManager.AllowTcpPort(kubeProxyHealthzPort); err != nil {
			return err
		}
		s.logger.Info("Allowing port on firewall", zap.Reflect("node-port-services",
			fmt.Sprintf("%s-%s", nodePortStartRangePort, nodePortEndRangePort)))
		if err = s.firewallManager.AllowTcpPortRange(nodePortStartRangePort, nodePortEndRangePort); err != nil {
			return err
		}
		s.logger.Info("Flushing firewall rules")
		if err = s.firewallManager.FlushRules(); err != nil {
			return err
		}
	} else {
		s.logger.Info("No firewall enabled on the host. Skipping setting firewall rules...")
	}
	return nil
}
