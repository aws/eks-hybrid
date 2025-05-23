package test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"

	. "github.com/onsi/gomega"
)

// GenerateCA creates a new CA certificate and returns the PEM encoded certificate,
// the parsed certificate, and the private key
func GenerateCA(g *WithT) ([]byte, *x509.Certificate, *ecdsa.PrivateKey) {
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

// GenerateKubeletCert creates a new kubelet certificate signed by the given CA
func GenerateKubeletCert(g *WithT, issuer *x509.Certificate, issuerKey *ecdsa.PrivateKey, validFrom, validTo time.Time) []byte {
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(2025),
		Subject: pkix.Name{
			Organization: []string{"Test Kubelet"},
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
