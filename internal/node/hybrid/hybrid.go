package hybrid

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"go.uber.org/zap"
	"k8s.io/utils/strings/slices"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/certificate"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/network"
	"github.com/aws/eks-hybrid/internal/kubernetes"
	"github.com/aws/eks-hybrid/internal/nodeprovider"
	"github.com/aws/eks-hybrid/internal/system"
	"github.com/aws/eks-hybrid/internal/validation"
)

const (
	nodeIpValidation            = "node-ip-validation"
	kubeletVersionSkew          = "kubelet-version-skew-validation"
	ntpSyncValidation           = "ntp-sync-validation"
	awsCredentialsValidation    = "aws-credentials-validation"
	apiServerEndpointResolution = "api-server-endpoint-resolution-validation"
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
		certPath:   kubelet.KubeletCurrentCertPath,
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

func (hnp *HybridNodeProvider) GetNodeConfig() *api.NodeConfig {
	return hnp.nodeConfig
}

func (hnp *HybridNodeProvider) Logger() *zap.Logger {
	return hnp.logger
}

func (hnp *HybridNodeProvider) Validate(ctx context.Context) error {
	if !slices.Contains(hnp.skipPhases, nodeIpValidation) {
		if err := hnp.ValidateNodeIP(); err != nil {
			return err
		}
	}

	if !slices.Contains(hnp.skipPhases, certificate.KubeletCertValidation) {
		hnp.logger.Info("Validating kubelet certificate...")
		if err := certificate.Validate(hnp.certPath, hnp.nodeConfig.Spec.Cluster.CertificateAuthority); err != nil {
			// Ignore date validation errors in the hybrid provider since kubelet will regenerate them
			// Ignore no cert errors since we expect it to not exist
			if certificate.IsDateValidationError(err) || certificate.IsNoCertError(err) {
				return nil
			}

			return certificate.AddKubeletRemediation(hnp.certPath, err)
		}
	}

	if !slices.Contains(hnp.skipPhases, kubeletVersionSkew) {
		if err := hnp.ValidateKubeletVersionSkew(); err != nil {
			return validation.WithRemediation(err,
				"Ensure the hybrid node's Kubernetes version follows the version skew policy of the EKS cluster. "+
					"Update the node's Kubernetes components using 'nodeadm upgrade' or reinstall with a compatible version. https://kubernetes.io/releases/version-skew-policy/#kubelet")
		}
	}

	if !slices.Contains(hnp.skipPhases, ntpSyncValidation) {
		hnp.logger.Info("Validating NTP synchronization...")
		ntpValidator := system.NewNTPValidator()
		if err := ntpValidator.Validate(); err != nil {
			return err
		}
	}

	if !slices.Contains(hnp.skipPhases, apiServerEndpointResolution) {
		hnp.logger.Info("Validating API Server endpoint connection...")
		connectionValidator := kubernetes.NewConnectionValidator()
		if err := connectionValidator.CheckConnection(ctx, hnp.nodeConfig); err != nil {
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
