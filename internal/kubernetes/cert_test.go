package kubernetes_test

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/kubernetes"
	"github.com/aws/eks-hybrid/internal/test"
)

func TestCheckKubeletCertificate(t *testing.T) {
	g := NewGomegaWithT(t)
	ctx := context.Background()

	caBytes, ca, caKey := test.GenerateCA(g)
	_, wrongCA, wrongCAKey := test.GenerateCA(g)

	// Create a mock EKS API server that returns cluster details
	cluster := &eks.DescribeClusterOutput{
		Cluster: &types.Cluster{
			Name:     aws.String("test-cluster"),
			Endpoint: aws.String("https://my-endpoint.example.com"),
			CertificateAuthority: &types.Certificate{
				Data: aws.String(base64.StdEncoding.EncodeToString(caBytes)),
			},
			Status: types.ClusterStatusActive,
			KubernetesNetworkConfig: &types.KubernetesNetworkConfigResponse{
				ServiceIpv4Cidr: aws.String("10.100.0.0/16"),
			},
		},
	}
	eksAPI := test.NewEKSDescribeClusterAPI(t, cluster)

	tests := []struct {
		name          string
		cert          []byte
		node          *api.NodeConfig
		config        aws.Config
		expectedError string
	}{
		{
			name: "success with direct CA",
			cert: test.GenerateKubeletCert(g, ca, caKey, time.Now(), time.Now().AddDate(10, 0, 0)),
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:                 "test-cluster",
						CertificateAuthority: caBytes,
						APIServerEndpoint:    "https://my-endpoint.example.com",
						CIDR:                 "10.100.0.0/16",
					},
				},
			},
			config:        aws.Config{},
			expectedError: "",
		},
		{
			name: "success with EKS API",
			cert: test.GenerateKubeletCert(g, ca, caKey, time.Now(), time.Now().AddDate(10, 0, 0)),
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:              "test-cluster",
						APIServerEndpoint: "https://my-endpoint.example.com",
					},
				},
			},
			config: aws.Config{
				BaseEndpoint: &eksAPI.URL,
				HTTPClient:   eksAPI.Client(),
			},
			expectedError: "",
		},
		{
			name: "missing file",
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:                 "test-cluster",
						CertificateAuthority: caBytes,
						APIServerEndpoint:    "https://my-endpoint.example.com",
						CIDR:                 "10.100.0.0/16",
					},
				},
			},
			config:        aws.Config{},
			expectedError: "no certificate found",
		},
		{
			name: "invalid format",
			cert: []byte("invalid-cert-data"),
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:                 "test-cluster",
						CertificateAuthority: caBytes,
						APIServerEndpoint:    "https://my-endpoint.example.com",
						CIDR:                 "10.100.0.0/16",
					},
				},
			},
			config:        aws.Config{},
			expectedError: "parsing certificate",
		},
		{
			name: "not yet valid certificate",
			cert: test.GenerateKubeletCert(g, ca, caKey, time.Now().AddDate(0, 0, 1), time.Now().AddDate(0, 0, 1)),
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:                 "test-cluster",
						CertificateAuthority: caBytes,
						APIServerEndpoint:    "https://my-endpoint.example.com",
						CIDR:                 "10.100.0.0/16",
					},
				},
			},
			config:        aws.Config{},
			expectedError: "server certificate is not yet valid",
		},
		{
			name: "expired certificate",
			cert: test.GenerateKubeletCert(g, ca, caKey, time.Now().AddDate(0, 0, -1), time.Now().AddDate(0, 0, -1)),
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:                 "test-cluster",
						CertificateAuthority: caBytes,
						APIServerEndpoint:    "https://my-endpoint.example.com",
						CIDR:                 "10.100.0.0/16",
					},
				},
			},
			config:        aws.Config{},
			expectedError: "server certificate has expired",
		},
		{
			name: "wrong CA",
			cert: test.GenerateKubeletCert(g, wrongCA, wrongCAKey, time.Now(), time.Now().AddDate(10, 0, 0)),
			node: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:                 "test-cluster",
						CertificateAuthority: caBytes,
						APIServerEndpoint:    "https://my-endpoint.example.com",
						CIDR:                 "10.100.0.0/16",
					},
				},
			},
			config:        aws.Config{},
			expectedError: "certificate is not valid for the current cluster",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			informer := test.NewFakeInformer()

			tmpDir := t.TempDir()
			certPath := filepath.Join(tmpDir, kubelet.KubeletCurrentCertPath)
			g.Expect(os.MkdirAll(filepath.Dir(certPath), 0o755)).To(Succeed())
			if tt.cert != nil {
				err := os.WriteFile(certPath, tt.cert, 0o600)
				g.Expect(err).NotTo(HaveOccurred())
			}

			err := kubernetes.NewKubeletCertificateValidator(kubernetes.NewClusterProvider(tt.config), kubernetes.WithCertPath(certPath)).Run(ctx, informer, tt.node)

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
