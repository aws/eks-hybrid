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
		name                string
		nodeConfig          *api.NodeConfig
		remoteNetworkConfig *eks.RemoteNetworkConfig
		wantErr             bool
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
			remoteNetworkConfig: &eks.RemoteNetworkConfig{
				RemoteNodeNetworks: []*eks.RemoteNodeNetwork{
					{
						CIDRs: []*string{aws.String("10.0.0.0/24")},
					},
				},
			},
			wantErr: false,
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
			remoteNetworkConfig: &eks.RemoteNetworkConfig{
				RemoteNodeNetworks: []*eks.RemoteNodeNetwork{
					{
						CIDRs: []*string{aws.String("10.0.0.0/24")},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "hostname override in remote node network",
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
			remoteNetworkConfig: &eks.RemoteNetworkConfig{
				RemoteNodeNetworks: []*eks.RemoteNodeNetwork{
					{
						CIDRs: []*string{aws.String("10.0.0.0/8")},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "hostname override not in remote node network",
			nodeConfig: &api.NodeConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:   "test-cluster",
						Region: "us-west-2",
					},
					Kubelet: api.KubeletOptions{
						Flags: []string{"--hostname-override=192.1.2.3"},
					},
				},
			},
			remoteNetworkConfig: &eks.RemoteNetworkConfig{
				RemoteNodeNetworks: []*eks.RemoteNodeNetwork{
					{
						CIDRs: []*string{aws.String("10.0.0.0/8")},
					},
				},
			},
			wantErr: true,
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
			remoteNetworkConfig: &eks.RemoteNetworkConfig{
				RemoteNodeNetworks: []*eks.RemoteNodeNetwork{
					{CIDRs: []*string{aws.String("10.1.0.0/16"), aws.String("192.1.0.0/24")}},
				},
			},
			wantErr: false,
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
			remoteNetworkConfig: &eks.RemoteNetworkConfig{
				RemoteNodeNetworks: []*eks.RemoteNodeNetwork{
					{CIDRs: []*string{aws.String("10.1.0.0/16"), aws.String("192.1.0.0/24")}},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			hnp, err := hybrid.NewHybridNodeProvider(
				tt.nodeConfig,
				zap.NewNop(),
				hybrid.WithRemoteNetworkConfig(tt.remoteNetworkConfig),
			)
			g.Expect(err).To(Succeed())

			err = hnp.ValidateNodeIP(context.Background())

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}
