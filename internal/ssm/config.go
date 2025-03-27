package ssm

import (
	"fmt"
	"os"
	"os/exec"

	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
)

type HybridInstanceRegistration struct {
	ManagedInstanceID string `json:"ManagedInstanceID"`
	Region            string `json:"Region"`
}

func (s *ssm) registerMachine(cfg *api.NodeConfig) error {
	registration := NewSSMRegistration()
	registered, err := registration.isRegistered()
	if err != nil {
		return err
	}

	if registered {
		s.logger.Info("SSM agent already registered, skipping registration")
	} else {
		agentPath, err := agentBinaryPath()
		if err != nil {
			return fmt.Errorf("can't register without ssm agent installed: %w", err)
		}

		s.logger.Info("Registering machine with SSM agent")
		registerCmd := exec.Command(agentPath,
			"-register", "-y",
			"-region", cfg.Spec.Cluster.Region,
			"-code", cfg.Spec.Hybrid.SSM.ActivationCode,
			"-id", cfg.Spec.Hybrid.SSM.ActivationID,
		)

		out, err := registerCmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("running register machine command: %s, error: %v", out, err)
		}
	}

	// Set the nodename on nodeconfig post registration
	registeredNodeName, err := registration.GetManagedHybridInstanceId()
	if err != nil {
		return err
	}

	s.logger.Info("Machine registered with SSM, assigning instance ID as node name", zap.String("instanceID", registeredNodeName))
	s.nodeConfig.Status.Hybrid.NodeName = registeredNodeName
	return nil
}

var possibleAgentPaths = []string{
	"/usr/bin/amazon-ssm-agent",
	"/snap/amazon-ssm-agent/current/amazon-ssm-agent",
}

func agentBinaryPath() (string, error) {
	for _, path := range possibleAgentPaths {
		if fileExists(path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("ssm agent binary not found in any of the well known paths [%s]", possibleAgentPaths)
}

func fileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}
