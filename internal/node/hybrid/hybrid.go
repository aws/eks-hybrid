package hybrid

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"go.uber.org/zap"
	"k8s.io/utils/strings/slices"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/nodeprovider"
	"github.com/aws/eks-hybrid/internal/system"
	"github.com/aws/eks-hybrid/internal/validation"
)

const (
	nodeIpValidation      = "node-ip-validation"
	kubeletCertValidation = "kubelet-cert-validation"
	kubeletVersionSkew    = "kubelet-version-skew-validation"
	ntpSyncValidation     = "ntp-sync-validation"
)

// ValidationRunner runs validations.
type ValidationRunner interface {
	Run(ctx context.Context, obj *api.NodeConfig, validations ...validation.Validation[*api.NodeConfig]) error
}

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
	kubelet  Kubelet
	// installRoot is optionally the root directory of the installation
	installRoot string
	runner      ValidationRunner
}

type NodeProviderOpt func(*HybridNodeProvider)

func NewHybridNodeProvider(nodeConfig *api.NodeConfig, skipPhases []string, logger *zap.Logger, opts ...NodeProviderOpt) (nodeprovider.NodeProvider, error) {
	var skipValidations []string
	for _, phase := range skipPhases {
		skipValidations = append(skipValidations, strings.TrimSuffix(phase, "-validation"))
	}

	np := &HybridNodeProvider{
		nodeConfig: nodeConfig,
		logger:     logger,
		skipPhases: skipPhases,
		network:    &defaultKubeletNetwork{},
		certPath:   kubelet.KubeletCurrentCertPath,
		kubelet:    kubelet.New(),
		runner: validation.NewSingleRunner[*api.NodeConfig](
			validation.NewZapInformer(logger),
			validation.WithSingleRunnerSkipValidations(skipValidations...),
		),
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

// WithKubelet adds a kubelet struct to the HybridNodeProvider for testing purposes.
func WithKubelet(kubelet Kubelet) NodeProviderOpt {
	return func(hnp *HybridNodeProvider) {
		hnp.kubelet = kubelet
	}
}

// WithValidationRunner sets the runner for the HybridNodeProvider.
func WithValidationRunner(runner ValidationRunner) NodeProviderOpt {
	return func(hnp *HybridNodeProvider) {
		hnp.runner = runner
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
		if err := ValidateCertificate(hnp.certPath, hnp.nodeConfig.Spec.Cluster.CertificateAuthority); err != nil {
			// Ignore date validation errors in the hybrid provider since kubelet will regenerate them
			// Ignore no cert errors since we expect it to not exist
			if IsDateValidationError(err) || IsNoCertError(err) {
				return nil
			}

			return AddKubeletRemediation(hnp.certPath, err)
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
		ntpValidator := system.NewNTPValidator(hnp.logger)
		if err := ntpValidator.Validate(); err != nil {
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

// AddKubeletRemediation adds kubelet-specific remediation messages based on error type
func AddKubeletRemediation(certPath string, err error) error {
	errWithContext := fmt.Errorf("validating kubelet certificate: %w", err)

	switch err.(type) {
	case *CertNotFoundError, *CertFileError, *CertReadError:
		return validation.WithRemediation(errWithContext, "Kubelet certificate will be created when the kubelet is able to authenticate with the API server. Check previous authentication remediation advice.")
	case *CertInvalidFormatError:
		return validation.WithRemediation(errWithContext, fmt.Sprintf("Delete the kubelet server certificate file %s and restart kubelet", certPath))
	case *CertClockSkewError:
		return validation.WithRemediation(errWithContext, "Verify the system time is correct and restart the kubelet.")
	case *CertExpiredError:
		return validation.WithRemediation(errWithContext, fmt.Sprintf("Delete the kubelet server certificate file %s and restart kubelet. Validate `serverTLSBootstrap` is true in the kubelet config /etc/kubernetes/kubelet/config.json to automatically rotate the certificate.", certPath))
	case *CertParseCAError:
		return validation.WithRemediation(errWithContext, "Ensure the cluster CA certificate is valid")
	case *CertInvalidCAError:
		return validation.WithRemediation(errWithContext, fmt.Sprintf("Please remove the kubelet server certificate file %s or use \"--skip %s\" if this is expected", certPath, kubeletCertValidation))
	}

	return errWithContext
}
