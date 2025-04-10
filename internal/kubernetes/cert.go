package kubernetes

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/validation"
)

const KubeletServerCertPath = "/var/lib/kubelet/pki/kubelet-server-current.pem"

type KubeletCertificateValidator struct {
	certPath string
}

func WithCertPath(certPath string) func(*KubeletCertificateValidator) {
	return func(v *KubeletCertificateValidator) {
		v.certPath = certPath
	}
}

func NewKubeletCertificateValidator(opts ...func(*KubeletCertificateValidator)) KubeletCertificateValidator {
	v := &KubeletCertificateValidator{
		certPath: KubeletServerCertPath,
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

	certPEM, err := os.ReadFile(v.certPath)
	if err != nil {
		remedationErr = validation.WithRemediation(fmt.Errorf("failed to read kubelet server certificate: %w", err),
			"Kubelet certificate will be created when the kubelet is able to authenticate with the API server. Check previous authentication remediation advice.")
		return remedationErr
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		remedationErr = validation.WithRemediation(fmt.Errorf("failed to parse kubelet server certificate"),
			fmt.Sprintf("Delete the kubelet server certificate file %s and restart kubelet", v.certPath))
		return remedationErr
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		remedationErr = validation.WithRemediation(fmt.Errorf("failed to parse kubelet server certificate: %w", err),
			fmt.Sprintf("Delete the kubelet server certificate file %s and restart kubelet", v.certPath))
		return remedationErr
	}

	now := time.Now()
	if now.Before(cert.NotBefore) {
		remedationErr = validation.WithRemediation(fmt.Errorf("kubelet server certificate is not yet valid (valid from %s)", cert.NotBefore),
			"Verify the system time is correct and restart the kubelet.")
		return remedationErr
	}
	if now.After(cert.NotAfter) {
		remedationErr = validation.WithRemediation(fmt.Errorf("kubelet server certificate has expired (expired at %s)", cert.NotAfter),
			fmt.Sprintf("Delete the kubelet server certificate file %s and restart kubelet. Validate `serverTLSBootstrap` is true in the kubelet config /etc/kubernetes/kubelet/config.json to automatically rotate the certificate.", v.certPath))
		return remedationErr
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(node.Spec.Cluster.CertificateAuthority) {
		remedationErr = validation.WithRemediation(fmt.Errorf("failed to parse cluster CA certificate"),
			"Ensure the cluster CA certificate is valid")
		return remedationErr
	}

	opts := x509.VerifyOptions{
		Roots:       caCertPool,
		CurrentTime: now,
	}
	if _, err := cert.Verify(opts); err != nil {
		remedationErr = validation.WithRemediation(fmt.Errorf("kubelet server certificate is not signed by the cluster CA: %w", err),
			fmt.Sprintf("Delete the kubelet server certificate file %s and restart kubelet", v.certPath))
		return remedationErr
	}

	return nil
}
