package ssm

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/system"
)

var (
	_             daemon.Daemon = &ssm{}
	SsmDaemonName               = "amazon-ssm-agent"

	activationExpiredRegex = regexp.MustCompile(`.*ActivationExpired*`)
	invalidActivationRegex = regexp.MustCompile(`.*InvalidActivation*`)
)

const (
	defaultAWSConfigPath   = "/root/.aws"
	awsCredentialsFilePath = defaultAWSConfigPath + "/credentials"
	eksHybridPath          = "/eks-hybrid"
	symlinkedAWSConfigPath = eksHybridPath + "/.aws"
)

type ssm struct {
	daemonManager daemon.DaemonManager
	nodeConfig    *api.NodeConfig
	logger        *zap.Logger
}

func NewSsmDaemon(daemonManager daemon.DaemonManager, cfg *api.NodeConfig, logger *zap.Logger) daemon.Daemon {
	setDaemonName()
	return &ssm{
		daemonManager: daemonManager,
		nodeConfig:    cfg,
		logger:        logger,
	}
}

func (s *ssm) Configure() error {
	if err := s.registerMachine(s.nodeConfig); err != nil {
		if match := activationExpiredRegex.MatchString(err.Error()); match {
			return fmt.Errorf("SSM activation expired. Please use a valid activation")
		} else if match := invalidActivationRegex.MatchString(err.Error()); match {
			return fmt.Errorf("invalid SSM activation. Please use a valid activation code, activation id and region")
		}
		return err
	}
	return nil
}

func (s *ssm) EnsureRunning(ctx context.Context) error {
	err := s.daemonManager.EnableDaemon(SsmDaemonName)
	if err != nil {
		return err
	}

	restartCancel, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	s.logger.Info("Restarting SSM agent...")
	// When the restart operation fails, it's usually because there are many operations running
	// for the same service, and we get rate limited. That's why we use a big backoff time.
	if err := daemon.RetryOperation(restartCancel, s.daemonManager.RestartDaemon, SsmDaemonName, 20*time.Second); err != nil {
		return fmt.Errorf("restarting SSM agent: %w", err)
	}

	runningCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	s.logger.Info("Waiting for SSM agent to be running...")
	if err := daemon.WaitForStatus(runningCtx, s.logger, s.daemonManager, SsmDaemonName, daemon.DaemonStatusRunning, 5*time.Second); err != nil {
		return fmt.Errorf("waiting for SSM agent to be running: %w", err)
	}
	s.logger.Info("SSM agent is running")

	return nil
}

func (s *ssm) PostLaunch() error {
	if s.nodeConfig.Spec.Hybrid.EnableCredentialsFile {
		s.logger.Info("Creating symlink for AWS credentials", zap.String("Symbolic link path", symlinkedAWSConfigPath))
		err := os.MkdirAll(eksHybridPath, 0o755)
		if err != nil {
			return fmt.Errorf("creating path: %v", err)
		}

		err = os.RemoveAll(symlinkedAWSConfigPath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing directory %s: %v", symlinkedAWSConfigPath, err)
		}

		err = os.Symlink(defaultAWSConfigPath, symlinkedAWSConfigPath)
		if err != nil {
			return fmt.Errorf("creating symlink: %v", err)
		}
	}

	return nil
}

// Stop stops the ssm unit only if it is loaded and running
func (s *ssm) Stop() error {
	return s.daemonManager.StopDaemon(SsmDaemonName)
}

func (s *ssm) Name() string {
	return SsmDaemonName
}

func setDaemonName() {
	osToDaemonName := map[string]string{
		system.UbuntuOsName: "snap.amazon-ssm-agent.amazon-ssm-agent",
		system.RhelOsName:   "amazon-ssm-agent",
		system.AmazonOsName: "amazon-ssm-agent",
	}
	osName := system.GetOsName()
	if daemonName, ok := osToDaemonName[osName]; ok {
		SsmDaemonName = daemonName
	}
}
