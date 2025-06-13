package hybrid

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"go.uber.org/zap"
	"k8s.io/utils/strings/slices"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/nodeprovider"
)

const (
	nodeIpValidation      = "node-ip-validation"
	kubeletCertValidation = "kubelet-cert-validation"
)

type HybridNodeProvider struct {
	nodeConfig    *api.NodeConfig
	validator     func(config *api.NodeConfig) error
	awsConfig     *aws.Config
	daemonManager daemon.DaemonManager
	logger        *zap.Logger
	cluster       *types.Cluster
	skipPhases    []string
	network       Network
	// CertPath is the path to the kubelet certificate
	// If not provided, defaults to kubelet.KubeletCurrentCertPath
	certPath string
}

type NodeProviderOpt func(*HybridNodeProvider)

func NewHybridNodeProvider(nodeConfig *api.NodeConfig, skipPhases []string, logger *zap.Logger, opts ...NodeProviderOpt) (nodeprovider.NodeProvider, error) {
	np := &HybridNodeProvider{
		nodeConfig: nodeConfig,
		logger:     logger,
		skipPhases: skipPhases,
		network:    &defaultKubeletNetwork{},
		certPath:   kubelet.KubeletCurrentCertPath,
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
func WithNetwork(network Network) NodeProviderOpt {
	return func(hnp *HybridNodeProvider) {
		hnp.network = network
	}
}

// WithCertPath sets the path to the kubelet certificate
func WithCertPath(path string) NodeProviderOpt {
	return func(hnp *HybridNodeProvider) {
		hnp.certPath = path
	}
}

func (hnp *HybridNodeProvider) GetNodeConfig() *api.NodeConfig {
	return hnp.nodeConfig
}

func (hnp *HybridNodeProvider) Logger() *zap.Logger {
	return hnp.logger
}

func (hnp *HybridNodeProvider) Validate() error {
	if !slices.Contains(hnp.skipPhases, nodeIpValidation) {
		if err := hnp.ValidateNodeIP(); err != nil {
			return err
		}
	}

	if !slices.Contains(hnp.skipPhases, kubeletCertValidation) {
		hnp.logger.Info("Validating kubelet certificate...")
		if err := ValidateCertificate(hnp.certPath, hnp.nodeConfig.Spec.Cluster.CertificateAuthority, CertTypeKubelet); err != nil {
			// Ignore date validation errors in the hybrid provider since kubelet will regenerate them
			// Ignore no cert errors since we expect it to not exist
			if IsDateValidationError(err) || IsNoCertError(err) {
				return nil
			}

			return err
		}
	}

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
