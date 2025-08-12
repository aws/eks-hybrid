package kubelet

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/kubernetes"
	"github.com/aws/eks-hybrid/internal/validation"
)

const (
	KubeletDaemonName                  = "kubelet"
	kubernetesAuthenticationValidation = "k8s-authentication-validation"
)

var _ daemon.Daemon = &kubelet{}

type CredentialProviderAwsConfig struct {
	Profile         string
	CredentialsPath string
}

type kubelet struct {
	daemonManager daemon.DaemonManager
	awsConfig     *aws.Config
	nodeConfig    *api.NodeConfig
	// environment variables to write for kubelet
	environment map[string]string
	// kubelet config flags without leading dashes
	flags                       map[string]string
	credentialProviderAwsConfig CredentialProviderAwsConfig
	validationRunner            *validation.Runner[*api.NodeConfig]
	logger                      *zap.Logger
}

func NewKubeletDaemon(daemonManager daemon.DaemonManager, cfg *api.NodeConfig, awsConfig *aws.Config, credentialProviderAwsConfig CredentialProviderAwsConfig, logger *zap.Logger, skipPhases []string) daemon.Daemon {
	kubeletDaemon := &kubelet{
		daemonManager:               daemonManager,
		nodeConfig:                  cfg,
		awsConfig:                   awsConfig,
		environment:                 make(map[string]string),
		flags:                       make(map[string]string),
		credentialProviderAwsConfig: credentialProviderAwsConfig,
		logger:                      logger,
	}

	if skipPhases != nil {
		kubeletDaemon.validationRunner = validation.NewRunner[*api.NodeConfig](validation.NewLoggerPrinterWithLogger(logger), validation.WithSkipValidations(skipPhases...))
	}

	return kubeletDaemon
}

func (k *kubelet) Configure(ctx context.Context) error {
	if err := k.writeKubeletConfig(); err != nil {
		return err
	}
	if err := k.writeKubeconfig(); err != nil {
		return err
	}
	if err := k.writeImageCredentialProviderConfig(); err != nil {
		return err
	}
	if err := writeClusterCaCert(k.nodeConfig.Spec.Cluster.CertificateAuthority); err != nil {
		return err
	}
	if err := k.writeKubeletEnvironment(); err != nil {
		return err
	}

	if k.validationRunner != nil {
		k.validationRunner.Register(
			validation.New(kubernetesAuthenticationValidation, kubernetes.NewAPIServerValidator(New()).MakeAuthenticatedRequest),
		)
		if err := k.validationRunner.Sequentially(ctx, k.nodeConfig); err != nil {
			return err
		}
	}

	return nil
}

func (k *kubelet) EnsureRunning(ctx context.Context) error {
	if err := k.daemonManager.DaemonReload(); err != nil {
		return err
	}
	if err := k.daemonManager.EnableDaemon(KubeletDaemonName); err != nil {
		return err
	}
	if err := k.daemonManager.RestartDaemon(ctx, KubeletDaemonName); err != nil {
		return err
	}

	runningCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	k.logger.Info("Waiting for kubelet to be running...")
	if err := daemon.WaitForStatus(runningCtx, k.logger, k.daemonManager, KubeletDaemonName, daemon.DaemonStatusRunning, 5*time.Second); err != nil {
		return fmt.Errorf("waiting for kubelet to be running: %w", err)
	}
	k.logger.Info("kubelet is running")

	return nil
}

func (k *kubelet) PostLaunch() error {
	return nil
}

func (k *kubelet) Stop() error {
	return k.daemonManager.StopDaemon(KubeletDaemonName)
}

func (k *kubelet) Name() string {
	return KubeletDaemonName
}
