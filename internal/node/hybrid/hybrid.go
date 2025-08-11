package hybrid

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/aws/sts"
	"github.com/aws/eks-hybrid/internal/creds"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/kubernetes"
	"github.com/aws/eks-hybrid/internal/network"
	"github.com/aws/eks-hybrid/internal/nodeprovider"
	"github.com/aws/eks-hybrid/internal/validation"
)

const (
	awsAuthValidation           = "aws-auth-validation"
	nodeIpValidation            = "node-ip-validation"
	kubeletCertValidation       = "kubelet-cert-validation"
	kubeletVersionSkew          = "kubelet-version-skew-validation"
	ntpSyncValidation           = "ntp-sync-validation"
	apiServerEndpointResolution = "api-server-endpoint-resolution-validation"
	proxyValidation             = "proxy-validation"
	nodeInactiveValidation      = "node-inactive-validation"
	kubeletCurrentCertPath      = "/var/lib/kubelet/pki/kubelet-server-current.pem"
)

type HybridNodeProvider struct {
	nodeConfig    *api.NodeConfig
	validator     func(config *api.NodeConfig) error
	awsConfig     *aws.Config
	daemonManager daemon.DaemonManager
	logger        *zap.Logger
	cluster       *types.Cluster
	skipPhases    []string
	network       network.Network
	// CertPath is the path to the kubelet certificate
	// If not provided, defaults to kubelet.KubeletCurrentCertPath
	certPath string
	kubelet  Kubelet
}

type NodeProviderOpt func(*HybridNodeProvider)

func NewHybridNodeProvider(nodeConfig *api.NodeConfig, skipPhases []string, logger *zap.Logger, opts ...NodeProviderOpt) (nodeprovider.NodeProvider, error) {
	np := &HybridNodeProvider{
		nodeConfig: nodeConfig,
		logger:     logger,
		skipPhases: skipPhases,
		network:    network.NewDefaultNetwork(),
		certPath:   kubeletCurrentCertPath,
		kubelet:    kubelet.New(),
	}
	np.withHybridValidators()
	if err := np.withDaemonManager(); err != nil {
		return nil, err
	}

	for _, opt := range opts {
		opt(np)
	}

	return np, nil
}

func WithAWSConfig(config *aws.Config) NodeProviderOpt {
	return func(hnp *HybridNodeProvider) {
		hnp.awsConfig = config
	}
}

// WithCluster adds an EKS cluster to the HybridNodeProvider for testing purposes.
func WithCluster(cluster *types.Cluster) NodeProviderOpt {
	return func(hnp *HybridNodeProvider) {
		hnp.cluster = cluster
	}
}

// WithNetwork adds network util functions to the HybridNodeProvider for testing purposes.
func WithNetwork(net network.Network) NodeProviderOpt {
	return func(hnp *HybridNodeProvider) {
		hnp.network = net
	}
}

// WithCertPath sets the path to the kubelet certificate
func WithCertPath(path string) NodeProviderOpt {
	return func(hnp *HybridNodeProvider) {
		hnp.certPath = path
	}
}

// WithKubelet adds a kubelet struct to the HybridNodeProvider for testing purposes.
func WithKubelet(kubelet Kubelet) NodeProviderOpt {
	return func(hnp *HybridNodeProvider) {
		hnp.kubelet = kubelet
	}
}

// WithDaemonManager adds a DaemonManager to the HybridNodeProvider for testing purposes.
func WithDaemonManager(dm daemon.DaemonManager) NodeProviderOpt {
	return func(hnp *HybridNodeProvider) {
		hnp.daemonManager = dm
	}
}

func (hnp *HybridNodeProvider) GetNodeConfig() *api.NodeConfig {
	return hnp.nodeConfig
}

func (hnp *HybridNodeProvider) Logger() *zap.Logger {
	return hnp.logger
}

func (hnp *HybridNodeProvider) Validate(ctx context.Context) error {
	// Create logger printer for structured validation logging
	printer := validation.NewLoggerPrinterWithLogger(hnp.logger)

	// Create validation runner with skip phases support
	runner := validation.NewRunner[*api.NodeConfig](printer, validation.WithSkipValidations(hnp.skipPhases...))

	// Register AWS credential validations if AWS config is available
	if hnp.awsConfig != nil {
		runner.Register(creds.Validations(*hnp.awsConfig, hnp.nodeConfig)...)
		runner.Register(
			validation.New(awsAuthValidation, sts.NewAuthenticationValidator(*hnp.awsConfig).Run),
		)
	}

	// Register all hybrid node validations
	runner.Register(
		validation.New(nodeIpValidation, network.NewNetworkInterfaceValidator(
			network.WithMTUValidation(false),
			network.WithCluster(hnp.cluster)).Run),
		validation.New(kubeletCertValidation, kubernetes.NewKubeletCertificateValidator(
			&hnp.nodeConfig.Spec.Cluster,
			kubernetes.WithCertPath(hnp.certPath),
			kubernetes.WithIgnoreDateAndNoCertErrors(true)).Run),
		validation.New(kubeletVersionSkew, hnp.ValidateKubeletVersionSkew),
		validation.New(apiServerEndpointResolution, kubernetes.ValidateAPIServerEndpointResolution),
		validation.New(proxyValidation, network.NewProxyValidator().Run),
		validation.New(nodeInactiveValidation, hnp.ValidateNodeIsInactive),
	)

	// Run all validations sequentially
	if err := runner.Sequentially(ctx, hnp.nodeConfig); err != nil {
		hnp.logger.Error("Hybrid node validation failures detected", zap.Error(err))
		return err
	}

	hnp.logger.Info("All hybrid node validations passed successfully")
	return nil
}

func (hnp *HybridNodeProvider) Cleanup() error {
	hnp.daemonManager.Close()
	return nil
}

// getCluster retrieves the Cluster object or makes a DescribeCluster call to the EKS API and caches the result if not already present
func (hnp *HybridNodeProvider) getCluster(ctx context.Context) (*types.Cluster, error) {
	if hnp.cluster != nil {
		return hnp.cluster, nil
	}

	cluster, err := readCluster(ctx, *hnp.awsConfig, hnp.nodeConfig)
	if err != nil {
		return nil, err
	}
	hnp.cluster = cluster

	return cluster, nil
}
