package kubernetes

import (
	"context"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/certificate"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/validation"
)

type KubeletCertificateValidator struct {
	// CertPath is the full path to the kubelet certificate
	certPath string
	cluster  *api.ClusterDetails
}

func WithCertPath(certPath string) func(*KubeletCertificateValidator) {
	return func(v *KubeletCertificateValidator) {
		v.certPath = certPath
	}
}

func NewKubeletCertificateValidator(cluster *api.ClusterDetails, opts ...func(*KubeletCertificateValidator)) KubeletCertificateValidator {
	v := &KubeletCertificateValidator{
		cluster:  cluster,
		certPath: kubelet.KubeletCurrentCertPath,
	}
	for _, opt := range opts {
		opt(v)
	}
	return *v
}

// Run validates the kubelet certificate against the cluster CA
// This function conforms to the validation framework signature
func (v KubeletCertificateValidator) Run(ctx context.Context, informer validation.Informer, node *api.NodeConfig) error {
	var err error
	nodeComplete := node.DeepCopy()
	nodeComplete.Spec.Cluster = *v.cluster

	name := "kubernetes-kubelet-certificate"
	informer.Starting(ctx, name, "Validating kubelet server certificate")
	defer func() {
		informer.Done(ctx, name, err)
	}()
	if err = certificate.Validate(v.certPath, node.Spec.Cluster.CertificateAuthority); err != nil {
		err = certificate.AddKubeletRemediation(v.certPath, err)
		return err
	}

	return nil
}
