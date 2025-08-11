package kubelet

import (
	"context"

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
}

func NewKubeletDaemon(daemonManager daemon.DaemonManager, cfg *api.NodeConfig, awsConfig *aws.Config, credentialProviderAwsConfig CredentialProviderAwsConfig, logger *zap.Logger, skipPhases []string) daemon.Daemon {
	kubeletDaemon := &kubelet{
		daemonManager:               daemonManager,
		nodeConfig:                  cfg,
		awsConfig:                   awsConfig,
		environment:                 make(map[string]string),
		flags:                       make(map[string]string),
		credentialProviderAwsConfig: credentialProviderAwsConfig,
	}

	if logger != nil && skipPhases != nil {
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
	err := k.daemonManager.EnableDaemon(KubeletDaemonName)
	if err != nil {
		return err
	}
	return k.daemonManager.RestartDaemon(ctx, KubeletDaemonName)
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
