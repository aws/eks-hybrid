package kubernetes

import (
	"context"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/certificate"
	"github.com/aws/eks-hybrid/internal/validation"
)

const kubeletCurrentCertPath = "/var/lib/kubelet/pki/kubelet-server-current.pem"

type KubeletCertificateValidator struct {
	// CertPath is the full path to the kubelet certificate
	certPath string
	cluster  *api.ClusterDetails
	// ignoreDateAndNoCertErrors controls whether to ignore date validation and no-cert errors
	ignoreDateAndNoCertErrors bool
}

func WithCertPath(certPath string) func(*KubeletCertificateValidator) {
	return func(v *KubeletCertificateValidator) {
		v.certPath = certPath
	}
}

func WithIgnoreDateAndNoCertErrors(ignore bool) func(*KubeletCertificateValidator) {
	return func(v *KubeletCertificateValidator) {
		v.ignoreDateAndNoCertErrors = ignore
	}
}

func NewKubeletCertificateValidator(cluster *api.ClusterDetails, opts ...func(*KubeletCertificateValidator)) KubeletCertificateValidator {
	v := &KubeletCertificateValidator{
		cluster:  cluster,
		certPath: kubeletCurrentCertPath,
	}
	for _, opt := range opts {
		opt(v)
	}
	return *v
}

// Run validates the kubelet certificate against the cluster CA
// This function conforms to the validation framework signature
func (v KubeletCertificateValidator) Run(ctx context.Context, informer validation.Informer, _ *api.NodeConfig) error {
	var err error
	name := "kubernetes-kubelet-certificate"
	informer.Starting(ctx, name, "Validating kubelet server certificate")
	defer func() {
		informer.Done(ctx, name, err)
	}()
	if err = certificate.Validate(v.certPath, v.cluster.CertificateAuthority); err != nil {
		if v.ignoreDateAndNoCertErrors && (certificate.IsDateValidationError(err) || certificate.IsNoCertError(err)) {
			// set error to nil for the informer to collect so that this validation does not error in the case
			// of a no-op handled error
			err = nil
			return err
		}
		err = certificate.AddKubeletRemediation(v.certPath, err)
		return err
	}

	return nil
}
