package kubernetes_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/kubernetes"
	"github.com/aws/eks-hybrid/internal/test"
)

func TestCheckKubeletCertificate(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := context.Background()

	caBytes, ca, caKey := generateCA(g)
	_, wrongCA, wrongCAKey := generateCA(g)
	tests := []struct {
		name          string
		cert          []byte
		expectedError string
	}{
		{
			name:          "success",
			cert:          generateKubeletCert(g, ca, caKey, time.Now(), time.Now().AddDate(10, 0, 0)),
			expectedError: "",
		},
		{
			name:          "missing file",
			expectedError: "failed to read kubelet server certificate",
		},
		{
			name:          "invalid format",
			cert:          []byte("invalid-cert-data"),
			expectedError: "failed to parse kubelet server certificate",
		},
		{
			name:          "not yet valid certificate",
			cert:          generateKubeletCert(g, ca, caKey, time.Now().AddDate(0, 0, 1), time.Now().AddDate(0, 0, 1)),
			expectedError: "kubelet server certificate is not yet valid",
		},
		{
			name:          "expired certificate",
			cert:          generateKubeletCert(g, ca, caKey, time.Now().AddDate(0, 0, -1), time.Now().AddDate(0, 0, -1)),
			expectedError: "kubelet server certificate has expired",
		},
		{
			name:          "wrong CA",
			cert:          generateKubeletCert(g, wrongCA, wrongCAKey, time.Now(), time.Now().AddDate(10, 0, 0)),
			expectedError: "kubelet server certificate is not signed by the cluster CA",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			informer := test.NewFakeInformer()

			node := &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						CertificateAuthority: caBytes,
					},
				},
			}

			tmpDir := t.TempDir()
			certPath := tmpDir + "/kubelet-server-current.pem"
			if tt.cert != nil {
				err := os.WriteFile(certPath, tt.cert, 0o600)
				g.Expect(err).NotTo(HaveOccurred())
			}

			err := kubernetes.NewKubeletCertificateValidator(kubernetes.WithCertPath(certPath)).Run(ctx, informer, node)

			if tt.expectedError == "" {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(informer.Started).To(BeTrue())
				g.Expect(informer.DoneWith).To(BeNil())
			} else {
				g.Expect(err).To(HaveOccurred())
				g.Expect(informer.Started).To(BeTrue())
				g.Expect(informer.DoneWith).To(MatchError(ContainSubstring(tt.expectedError)))
			}
		})
	}
}

func generateCA(g *WithT) ([]byte, *x509.Certificate, *ecdsa.PrivateKey) {
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(2025),
		Subject: pkix.Name{
			Organization: []string{"Test CA"},
			CommonName:   "test-ca",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	g.Expect(err).NotTo(HaveOccurred())

	certBytes, err := x509.CreateCertificate(rand.Reader, cert, cert, &privateKey.PublicKey, privateKey)
	g.Expect(err).NotTo(HaveOccurred())

	certPEM := new(bytes.Buffer)
	g.Expect(pem.Encode(certPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})).NotTo(HaveOccurred())

	return certPEM.Bytes(), cert, privateKey
}

func generateKubeletCert(g *WithT, issuer *x509.Certificate, issuerKey *ecdsa.PrivateKey, validFrom, validTo time.Time) []byte {
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(2025),
		Subject: pkix.Name{
			Organization: []string{"Test 	Kubelet"},
			CommonName:   "test-kubelet",
		},
		NotBefore:             validFrom,
		NotAfter:              validTo,
		IsCA:                  false,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	g.Expect(err).NotTo(HaveOccurred())

	certBytes, err := x509.CreateCertificate(rand.Reader, cert, issuer, &privateKey.PublicKey, issuerKey)
	g.Expect(err).NotTo(HaveOccurred())

	certPEM := new(bytes.Buffer)
	g.Expect(pem.Encode(certPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})).NotTo(HaveOccurred())

	return certPEM.Bytes()
}
