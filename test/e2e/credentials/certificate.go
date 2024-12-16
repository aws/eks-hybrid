package credentials

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"time"
)

type Certificate struct {
	Cert    *x509.Certificate `json:"cert"`
	CertPEM []byte            `json:"certPEM"`
	Key     *ecdsa.PrivateKey `json:"key"`
	KeyPEM  []byte            `json:"keyPEM"`
}

func CreateCA() (*Certificate, error) {
	caCertFile := "ca.crt"
	caKeyFile := "ca.key"

	if fileExists(caCertFile) && fileExists(caKeyFile) {
		return readCertificate(caCertFile, caKeyFile)
	}

	now := time.Now()
	privateKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating private key for CA: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generating serial number for CA: %w", err)
	}
	ca := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Hybrid Nodes Corp."},
			Country:      []string{"US"},
			Locality:     []string{"Chicago"},
		},
		NotBefore:             now,
		NotAfter:              now.AddDate(10, 0, 0), // 10 years
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
	}

	caBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, fmt.Errorf("creating CA certificate: %w", err)
	}

	caPEM := new(bytes.Buffer)
	err = pem.Encode(caPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("encoding CA certificate: %w", err)
	}

	privateKeyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("marshaling private key: %w", err)
	}

	keyPEM := new(bytes.Buffer)
	if err := pem.Encode(keyPEM, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privateKeyBytes}); err != nil {
		return nil, fmt.Errorf("encoding private key: %w", err)
	}

	if err := os.WriteFile(caCertFile, caPEM.Bytes(), 0o600); err != nil {
		return nil, fmt.Errorf("writing CA cert to file: %w", err)
	}

	if err := os.WriteFile(caKeyFile, keyPEM.Bytes(), 0o600); err != nil {
		return nil, fmt.Errorf("writing CA key to file: %w", err)
	}

	return &Certificate{
		CertPEM: caPEM.Bytes(),
		Cert:    ca,
		Key:     privateKey,
		KeyPEM:  keyPEM.Bytes(),
	}, nil
}

// CreateCertificateForNode creates a new certificate with the nodeName as the Subject's CN.
func CreateCertificateForNode(ca *x509.Certificate, caPrivKey any, nodeName string) (*Certificate, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating private key for certificate: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generating serial number for certificate: %w", err)
	}
	now := time.Now()
	cert := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Hybrid Nodes Corp."},
			Country:      []string{"US"},
			Locality:     []string{"Chicago"},
			CommonName:   nodeName,
		},
		NotBefore:             now,
		NotAfter:              now.AddDate(1, 0, 0), // 1 years
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, cert, ca, &privateKey.PublicKey, caPrivKey)
	if err != nil {
		return nil, fmt.Errorf("creating CA certificate: %w", err)
	}

	certPEM := new(bytes.Buffer)
	err = pem.Encode(certPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("encoding certificate: %w", err)
	}

	privateKeyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("marshaling private key: %w", err)
	}

	keyPEM := new(bytes.Buffer)
	if err := pem.Encode(keyPEM, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privateKeyBytes}); err != nil {
		return nil, fmt.Errorf("encoding private key: %w", err)
	}

	return &Certificate{
		CertPEM: certPEM.Bytes(),
		Cert:    cert,
		Key:     privateKey,
		KeyPEM:  keyPEM.Bytes(),
	}, nil
}

func fileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}

func readCertificate(certPath, keyPath string) (*Certificate, error) {
	certEncoded, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}

	keyEncoded, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}

	return ParseCertificate(certEncoded, keyEncoded)
}

func ParseCertificate(certPEM, keyPEM []byte) (*Certificate, error) {
	certDecoded, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(certDecoded.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing cert: %w", err)
	}

	keyDecoded, _ := pem.Decode(keyPEM)
	key, err := x509.ParseECPrivateKey(keyDecoded.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing key: %w", err)
	}

	return &Certificate{
		Cert:    cert,
		CertPEM: certPEM,
		Key:     key,
		KeyPEM:  keyPEM,
	}, nil
}
