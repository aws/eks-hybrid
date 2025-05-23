package kubernetes

import (
	"context"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/node/hybrid"
	"github.com/aws/eks-hybrid/internal/validation"
)

type KubeletCertificateValidator struct {
	// InstallRoot is optionally the root directory of the installation
	// If not provided, the installRoot will be empty representing the root (/) of the fs
	installRoot     string
	clusterProvider ClusterProvider
}

func WithInstallRoot(installRoot string) func(*KubeletCertificateValidator) {
	return func(v *KubeletCertificateValidator) {
		v.installRoot = installRoot
	}
}

func NewKubeletCertificateValidator(clusterProvider ClusterProvider, opts ...func(*KubeletCertificateValidator)) KubeletCertificateValidator {
	v := &KubeletCertificateValidator{
		clusterProvider: clusterProvider,
	}
	for _, opt := range opts {
		opt(v)
	}
	return *v
}

func (v KubeletCertificateValidator) Run(ctx context.Context, informer validation.Informer, node *api.NodeConfig) error {
	name := "kubernetes-kubelet-certificate"
	var remedationErr error
	informer.Starting(ctx, name, "Validating kubelet server certificate")
	defer func() {
		informer.Done(ctx, name, remedationErr)
	}()

	cluster, err := v.clusterProvider.ReadClusterDetails(ctx, node, informer)
	if err != nil {
		remedationErr = err
		return remedationErr
	}

	if err := hybrid.ValidateKubeletCert(v.installRoot, cluster.CertificateAuthority); err != nil {
		remedationErr = hybrid.AsValidationError(err)
		return remedationErr
	}

	return nil
}
