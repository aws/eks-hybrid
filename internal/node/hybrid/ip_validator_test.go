package hybrid_test

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/aws/eks"
	"github.com/aws/eks-hybrid/internal/node/hybrid"
)

func TestHybridNodeProvider_ValidateNodeIP(t *testing.T) {
	tests := []struct {
		name        string
		nodeConfig  *api.NodeConfig
		cluster     *eks.Cluster
		expectedErr string
	}{
		{
			name: "valid node-ip flag in remote node network",
			nodeConfig: &api.NodeConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:   "test-cluster",
						Region: "us-west-2",
					},
					Kubelet: api.KubeletOptions{
						Flags: []string{"--node-ip=10.0.0.3"},
					},
				},
			},
			cluster: &eks.Cluster{
				Name: aws.String("test-cluster"),
				RemoteNetworkConfig: &eks.RemoteNetworkConfig{
					RemoteNodeNetworks: []*eks.RemoteNodeNetwork{
						{
							CIDRs: []*string{aws.String("10.0.0.0/24")},
						},
					},
				},
			},
			expectedErr: "",
		},
		{
			name: "node-ip flag not in remote node network",
			nodeConfig: &api.NodeConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:   "test-cluster",
						Region: "us-west-2",
					},
					Kubelet: api.KubeletOptions{
						Flags: []string{"--node-ip=192.168.1.1"},
					},
				},
			},
			cluster: &eks.Cluster{
				Name: aws.String("test-cluster"),
				RemoteNetworkConfig: &eks.RemoteNetworkConfig{
					RemoteNodeNetworks: []*eks.RemoteNodeNetwork{
						{
							CIDRs: []*string{aws.String("10.0.0.0/24")},
						},
					},
				},
			},
			expectedErr: "node IP 192.168.1.1 is not in any of the remote network CIDR blocks: [10.0.0.0/24]",
		},
		{
			name: "ip in one of multiple remote node networks",
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Kubelet: api.KubeletOptions{
						Flags: []string{"--node-ip=192.1.0.20"},
					},
				},
			},
			cluster: &eks.Cluster{
				RemoteNetworkConfig: &eks.RemoteNetworkConfig{
					RemoteNodeNetworks: []*eks.RemoteNodeNetwork{
						{CIDRs: []*string{aws.String("10.1.0.0/16"), aws.String("192.1.0.0/24")}},
					},
				},
			},
			expectedErr: "",
		},
		{
			name: "ip not in one of multiple remote node networks",
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Kubelet: api.KubeletOptions{
						Flags: []string{"--node-ip=178.1.2.3"},
					},
				},
			},
			cluster: &eks.Cluster{
				RemoteNetworkConfig: &eks.RemoteNetworkConfig{
					RemoteNodeNetworks: []*eks.RemoteNodeNetwork{
						{CIDRs: []*string{aws.String("10.1.0.0/16"), aws.String("192.1.0.0/24")}},
					},
				},
			},
			expectedErr: "node IP 178.1.2.3 is not in any of the remote network CIDR blocks: [10.1.0.0/16 192.1.0.0/24]",
		},
		{
			name: "hostname override present",
			nodeConfig: &api.NodeConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:   "test-cluster",
						Region: "us-west-2",
					},
					Kubelet: api.KubeletOptions{
						Flags: []string{"--hostname-override=10.1.2.3"},
					},
				},
			},
			cluster: &eks.Cluster{
				Name: aws.String("test-cluster"),
				RemoteNetworkConfig: &eks.RemoteNetworkConfig{
					RemoteNodeNetworks: []*eks.RemoteNodeNetwork{
						{
							CIDRs: []*string{aws.String("10.0.0.0/8")},
						},
					},
				},
			},
			expectedErr: "hostname-override kubelet flag is not supported for hybrid nodes but found override:  10.1.2.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			hnp, err := hybrid.NewHybridNodeProvider(
				tt.nodeConfig,
				zap.NewNop(),
				hybrid.WithCluster(tt.cluster),
			)
			g.Expect(err).To(Succeed())

			err = hnp.Validate(context.Background(), []string{})

			if tt.expectedErr != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.expectedErr))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}
