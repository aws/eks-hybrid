package kubernetes

import (
	"context"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/node/hybrid"
	"github.com/aws/eks-hybrid/internal/validation"
)

type KubeletCertificateValidator struct {
	// CertPath is the full path to the kubelet certificate
	certPath        string
	clusterProvider ClusterProvider
}

func WithCertPath(certPath string) func(*KubeletCertificateValidator) {
	return func(v *KubeletCertificateValidator) {
		v.certPath = certPath
	}
}

func NewKubeletCertificateValidator(clusterProvider ClusterProvider, opts ...func(*KubeletCertificateValidator)) KubeletCertificateValidator {
	v := &KubeletCertificateValidator{
		clusterProvider: clusterProvider,
		certPath:        kubelet.KubeletCurrentCertPath,
	}
	for _, opt := range opts {
		opt(v)
	}
	return *v
}

func (v KubeletCertificateValidator) Run(ctx context.Context, informer validation.Informer, node *api.NodeConfig) error {
	cluster, err := v.clusterProvider.ReadClusterDetails(ctx, node)
	if err != nil {
		// Only if reading the EKS fail is when we "start" a validation and signal it as failed.
		// Otherwise, there is no need to surface we are reading from the EKS API.
		informer.Starting(ctx, "kubernetes-endpoint-access", "Validating access to Kubernetes API endpoint")
		informer.Done(ctx, "kubernetes-endpoint-access", err)
		return err
	}

	name := "kubernetes-kubelet-certificate"
	informer.Starting(ctx, name, "Validating kubelet server certificate")
	defer func() {
		informer.Done(ctx, name, err)
	}()
	if err = hybrid.ValidateCertificate(v.certPath, cluster.CertificateAuthority); err != nil {
		err = hybrid.AddKubeletRemediation(v.certPath, err)
		return err
	}

	return nil
}
