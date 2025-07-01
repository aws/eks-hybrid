package hybrid_test

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/node/hybrid"
)

func TestHybridNodeProvider_ValidateKubeletVersionSkew(t *testing.T) {
	tests := []struct {
		name           string
		cluster        *types.Cluster
		kubeletVersion string
		expectedErr    string
		kubeletError   error
	}{
		{
			name: "kubelet same version",
			cluster: &types.Cluster{
				Version: aws.String("1.30"),
			},
			kubeletVersion: "v1.30.11",
			expectedErr:    "",
		},
		{
			name: "kubelet one version behind",
			cluster: &types.Cluster{
				Version: aws.String("1.30"),
			},
			kubeletVersion: "v1.29.5",
			expectedErr:    "",
		},
		{
			name: "kubelet two versions behind",
			cluster: &types.Cluster{
				Version: aws.String("1.30"),
			},
			kubeletVersion: "v1.28.10",
			expectedErr:    "",
		},
		{
			name: "kubelet three versions behind (max skew)",
			cluster: &types.Cluster{
				Version: aws.String("1.30"),
			},
			kubeletVersion: "v1.27.0",
			expectedErr:    "",
		},
		{
			name:           "nil cluster skips validation",
			cluster:        nil,
			kubeletVersion: "v1.25.0",
			expectedErr:    "",
		},
		{
			name: "kubelet newer than apiserver",
			cluster: &types.Cluster{
				Version: aws.String("1.29"),
			},
			kubeletVersion: "v1.30.0",
			expectedErr:    "kubelet version v1.30.0 is newer than kube-apiserver version 1.29",
		},
		{
			name: "kubelet too old (exceeds max skew)",
			cluster: &types.Cluster{
				Version: aws.String("1.31"),
			},
			kubeletVersion: "v1.27.5",
			expectedErr:    "kubelet version v1.27.5 is too old for kube-apiserver version 1.31",
		},
		{
			name: "invalid apiserver version",
			cluster: &types.Cluster{
				Version: aws.String("invalid"),
			},
			kubeletVersion: "v1.30.0",
			expectedErr:    "failed to parse kube-apiserver version",
		},
		{
			name: "invalid kubelet version",
			cluster: &types.Cluster{
				Version: aws.String("1.30"),
			},
			kubeletVersion: "invalid",
			expectedErr:    "failed to parse kubelet version",
		},
		{
			name: "failed getting kubelet version",
			cluster: &types.Cluster{
				Version: aws.String("1.30"),
			},
			kubeletVersion: "v1.30.0",
			kubeletError:   fmt.Errorf("failed"),
			expectedErr:    "failed to get kubelet version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			mockKubelet := newMockKubelet(tt.kubeletVersion, tt.kubeletError)
			hnp, err := hybrid.NewHybridNodeProvider(
				&api.NodeConfig{},
				[]string{"node-ip-validation", "kubelet-cert-validation"},
				zap.NewNop(),
				hybrid.WithCluster(tt.cluster),
				hybrid.WithKubelet(mockKubelet),
			)
			g.Expect(err).To(Succeed())

			err = hnp.Validate()
			if tt.expectedErr != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.expectedErr))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

// mockKubelet implements the Kubelet interface for testing
type mockKubelet struct {
	version      string
	versionError error
}

func newMockKubelet(version string, versionError error) *mockKubelet {
	return &mockKubelet{
		version:      version,
		versionError: versionError,
	}
}

func (m *mockKubelet) Version() (string, error) {
	if m.versionError != nil {
		return "", m.versionError
	}
	return m.version, nil
}
