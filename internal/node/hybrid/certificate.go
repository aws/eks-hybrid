package hybrid

import (
	"context"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/certificate"
	"github.com/aws/eks-hybrid/internal/validation"
)

func (hnp *HybridNodeProvider) ValidateCertificateIfExists(ctx context.Context, informer validation.Informer, node *api.NodeConfig) error {
	var err error
	name := kubeletCertValidation
	informer.Starting(ctx, name, "Validating kubelet server certificate")
	defer func() {
		informer.Done(ctx, name, err)
	}()
	if err = certificate.Validate(hnp.certPath, node.Spec.Cluster.CertificateAuthority); err != nil {
		if certificate.IsDateValidationError(err) || certificate.IsNoCertError(err) {
			return nil
		}
		err = certificate.AddKubeletRemediation(hnp.certPath, err)
		return err
	}

	return nil
}
