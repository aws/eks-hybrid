package hybrid

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/test"
)

func TestValidateKubeletCert(t *testing.T) {
	g := NewGomegaWithT(t)
	logger := zap.NewNop()

	caBytes, ca, caKey := test.GenerateCA(g)
	_, wrongCA, wrongCAKey := test.GenerateCA(g)

	tests := []struct {
		name          string
		cert          []byte
		ca            []byte
		skipPhases    []string
		expectedError string
		setup         func(tempDir string) error
	}{
		{
			name:          "no existing cert",
			expectedError: "",
		},
		{
			name:          "valid existing cert",
			cert:          test.GenerateKubeletCert(g, ca, caKey, time.Now(), time.Now().AddDate(10, 0, 0)),
			ca:            caBytes,
			expectedError: "",
		},
		{
			name:          "expired certificate",
			cert:          test.GenerateKubeletCert(g, ca, caKey, time.Now().AddDate(0, 0, -1), time.Now().AddDate(0, 0, -1)),
			ca:            caBytes,
			expectedError: "",
		},
		{
			name:          "wrong CA",
			cert:          test.GenerateKubeletCert(g, wrongCA, wrongCAKey, time.Now(), time.Now().AddDate(10, 0, 0)),
			ca:            caBytes,
			expectedError: "certificate is not valid for the current cluster",
		},
		{
			name:          "skip validation",
			cert:          test.GenerateKubeletCert(g, wrongCA, wrongCAKey, time.Now(), time.Now().AddDate(10, 0, 0)),
			ca:            caBytes,
			skipPhases:    []string{kubeletCertValidation},
			expectedError: "",
		},
		{
			name:          "stat error",
			expectedError: "reading certificate",
			setup: func(tempDir string) error {
				// Create a directory with the same name as the cert file to cause a stat error
				certPath := filepath.Join(tempDir, kubelet.KubeletCurrentCertPath)
				return os.MkdirAll(certPath, 0o755)
			},
		},
		{
			name:          "invalid cert format",
			cert:          []byte("invalid pem data"),
			ca:            caBytes,
			expectedError: "parsing certificate",
		},
		{
			name:          "invalid CA format",
			cert:          test.GenerateKubeletCert(g, ca, caKey, time.Now(), time.Now().AddDate(10, 0, 0)),
			ca:            []byte("invalid ca data"),
			expectedError: "parsing cluster CA certificate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			// Create a temporary directory for test files
			tempDir, err := os.MkdirTemp("", "hybrid-test-*")
			g.Expect(err).NotTo(HaveOccurred())
			defer os.RemoveAll(tempDir)

			// Create the directory structure for the kubelet cert
			certDir := filepath.Join(tempDir, filepath.Dir(kubelet.KubeletCurrentCertPath))
			g.Expect(os.MkdirAll(certDir, 0o755)).To(Succeed())

			if tt.setup != nil {
				g.Expect(tt.setup(tempDir)).To(Succeed())
			}

			certPath := filepath.Join(tempDir, kubelet.KubeletCurrentCertPath)
			if tt.cert != nil {
				err := os.WriteFile(certPath, tt.cert, 0o600)
				g.Expect(err).NotTo(HaveOccurred())
			}

			nodeConfig := &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						CertificateAuthority: tt.ca,
					},
				},
			}

			provider, err := NewHybridNodeProvider(nodeConfig, tt.skipPhases, logger, WithCertPath(certPath))
			g.Expect(err).NotTo(HaveOccurred())

			err = provider.Validate()
			if tt.expectedError == "" {
				g.Expect(err).NotTo(HaveOccurred())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(tt.expectedError)))
			}
		})
	}
}
