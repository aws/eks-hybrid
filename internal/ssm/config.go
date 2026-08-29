package ssm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/util/cmd"
)

const (
	// SSMRegistrationTimeout is the maximum time to wait for SSM registration to complete
	SSMRegistrationTimeout = 60 * time.Second

	// SSMRegistrationBackoff is the time to wait between registration retry attempts
	SSMRegistrationBackoff = 10 * time.Second
)

type HybridInstanceRegistration struct {
	ManagedInstanceID string `json:"ManagedInstanceID"`
	Region            string `json:"Region"`
}

func (s *ssm) registerMachine(ctx context.Context, cfg *api.NodeConfig) error {
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

		cmdBuilder := func(ctx context.Context) *exec.Cmd {
			return exec.CommandContext(ctx, agentPath,
				"-register", "-y",
				"-region", cfg.Spec.Cluster.Region,
				"-code", cfg.Spec.Hybrid.SSM.ActivationCode,
				"-id", cfg.Spec.Hybrid.SSM.ActivationID,
			)
		}

		if err := cmd.Retry(ctx, cmdBuilder, SSMRegistrationBackoff); err != nil {
			return fmt.Errorf("failed to register machine with SSM after multiple attempts: %w", err)
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
	snapAgentBinaryPath,
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
