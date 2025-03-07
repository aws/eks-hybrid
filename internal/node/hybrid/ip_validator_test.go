package hybrid_test

import (
	"fmt"
	"net"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/aws/eks"
	"github.com/aws/eks-hybrid/internal/node/hybrid"
)

func TestHybridNodeProvider_ValidateNodeIP(t *testing.T) {
	tests := []struct {
		name         string
		nodeConfig   *api.NodeConfig
		cluster      *eks.Cluster
		networkUtils hybrid.IPValidationNetworkUtils
		expectedErr  string
	}{
		{
			name: "valid node-ip flag in remote node network",
			nodeConfig: &api.NodeConfig{
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
			networkUtils: &MockIPValidationNetworkUtils{
				DNSRecords:       map[string][]net.IP{},
				ResolvedBindAddr: net.ParseIP("192.1.1.1"),
				BindAddrErr:      nil,
				NetworkInterfaces: []net.Addr{
					&net.IPNet{
						IP:   net.ParseIP("192.1.1.1"),
						Mask: net.CIDRMask(24, 32),
					},
					&net.IPNet{
						IP:   net.ParseIP("10.0.0.3"),
						Mask: net.CIDRMask(24, 32),
					},
				},
			},
			expectedErr: "",
		},
		{
			name: "node-ip flag not in remote node network",
			nodeConfig: &api.NodeConfig{
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
			networkUtils: &MockIPValidationNetworkUtils{
				DNSRecords:       map[string][]net.IP{},
				ResolvedBindAddr: net.ParseIP("192.1.1.1"),
				BindAddrErr:      nil,
				NetworkInterfaces: []net.Addr{
					&net.IPNet{
						IP:   net.ParseIP("192.1.1.1"),
						Mask: net.CIDRMask(24, 32),
					},
					&net.IPNet{
						IP:   net.ParseIP("192.168.1.1"),
						Mask: net.CIDRMask(24, 32),
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
			networkUtils: &MockIPValidationNetworkUtils{
				DNSRecords:       map[string][]net.IP{},
				ResolvedBindAddr: net.ParseIP("192.1.1.1"),
				BindAddrErr:      nil,
				NetworkInterfaces: []net.Addr{
					&net.IPNet{
						IP:   net.ParseIP("192.1.1.1"),
						Mask: net.CIDRMask(24, 32),
					},
					&net.IPNet{
						IP:   net.ParseIP("192.1.0.20"),
						Mask: net.CIDRMask(24, 32),
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
			networkUtils: &MockIPValidationNetworkUtils{
				DNSRecords:       map[string][]net.IP{},
				ResolvedBindAddr: net.ParseIP("10.1.0.0"),
				BindAddrErr:      nil,
				NetworkInterfaces: []net.Addr{
					&net.IPNet{
						IP:   net.ParseIP("10.1.0.0"),
						Mask: net.CIDRMask(24, 32),
					},
					&net.IPNet{
						IP:   net.ParseIP("178.1.2.3"),
						Mask: net.CIDRMask(24, 32),
					},
				},
			},
			expectedErr: "node IP 178.1.2.3 is not in any of the remote network CIDR blocks: [10.1.0.0/16 192.1.0.0/24]",
		},
		{
			name: "Invalid node-ip flag",
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Kubelet: api.KubeletOptions{
						Flags: []string{"--node-ip=invalid-ip"},
					},
				},
			},
			cluster: &eks.Cluster{
				RemoteNetworkConfig: &eks.RemoteNetworkConfig{
					RemoteNodeNetworks: []*eks.RemoteNodeNetwork{
						{CIDRs: []*string{aws.String("10.0.0.0/24")}},
					},
				},
			},
			networkUtils: &MockIPValidationNetworkUtils{
				DNSRecords:       map[string][]net.IP{},
				ResolvedBindAddr: nil,
				BindAddrErr:      nil,
				NetworkInterfaces: []net.Addr{
					&net.IPNet{
						IP:   net.ParseIP("10.0.0.1"),
						Mask: net.CIDRMask(24, 32),
					},
				},
			},
			expectedErr: "invalid ip invalid-ip in --node-ip flag. only 1 IPv4 address is allowed",
		},
		{
			name: "node ip found via DNS within remote node network",
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:   "test-cluster",
						Region: "us-west-2",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:        "node1.example.com",
							TrustAnchorARN:  "arn:aws:rolesanywhere:us-west-2:123456789012:trust-anchor/abcdef12-3456-7890-abcd-ef1234567890",
							ProfileARN:      "arn:aws:rolesanywhere:us-west-2:123456789012:profile/fedcba98-7654-3210-fedc-ba9876543210",
							RoleARN:         "arn:aws:iam::123456789012:role/EKSHybridNodeRole",
							CertificatePath: "/etc/eks/certs/node-cert.pem",
							PrivateKeyPath:  "/etc/eks/certs/node-key.pem",
						},
					},
				},
				Status: api.NodeConfigStatus{
					Hybrid: api.HybridDetails{
						NodeName: "node1.example.com",
					},
				},
			},
			cluster: &eks.Cluster{
				RemoteNetworkConfig: &eks.RemoteNetworkConfig{
					RemoteNodeNetworks: []*eks.RemoteNodeNetwork{
						{CIDRs: []*string{aws.String("10.0.0.0/24")}},
					},
				},
			},
			networkUtils: &MockIPValidationNetworkUtils{
				DNSRecords: map[string][]net.IP{
					"node1.example.com": {net.ParseIP("10.0.0.5")},
				},
				ResolvedBindAddr: nil,
				BindAddrErr:      fmt.Errorf("should not be called"),
				NetworkInterfaces: []net.Addr{
					&net.IPNet{
						IP:   net.ParseIP("10.0.0.5"),
						Mask: net.CIDRMask(24, 32),
					},
				},
			},
			expectedErr: "",
		},
		{
			name: "node ip found outside of DNS within remote node network",
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:   "test-cluster",
						Region: "us-west-2",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:        "node1.example.com",
							TrustAnchorARN:  "arn:aws:rolesanywhere:us-west-2:123456789012:trust-anchor/abcdef12-3456-7890-abcd-ef1234567890",
							ProfileARN:      "arn:aws:rolesanywhere:us-west-2:123456789012:profile/fedcba98-7654-3210-fedc-ba9876543210",
							RoleARN:         "arn:aws:iam::123456789012:role/EKSHybridNodeRole",
							CertificatePath: "/etc/eks/certs/node-cert.pem",
							PrivateKeyPath:  "/etc/eks/certs/node-key.pem",
						},
					},
				},
				Status: api.NodeConfigStatus{
					Hybrid: api.HybridDetails{
						NodeName: "node1.example.com",
					},
				},
			},
			cluster: &eks.Cluster{
				RemoteNetworkConfig: &eks.RemoteNetworkConfig{
					RemoteNodeNetworks: []*eks.RemoteNodeNetwork{
						{CIDRs: []*string{aws.String("10.0.0.0/24")}},
					},
				},
			},
			networkUtils: &MockIPValidationNetworkUtils{
				DNSRecords: map[string][]net.IP{
					"node1.example.com": {net.ParseIP("192.168.1.1")},
				},
				ResolvedBindAddr: nil,
				BindAddrErr:      fmt.Errorf("should not be called"),
				NetworkInterfaces: []net.Addr{
					&net.IPNet{
						IP:   net.ParseIP("192.168.1.1"),
						Mask: net.CIDRMask(16, 32),
					},
				},
			},
			expectedErr: "node IP 192.168.1.1 is not in any of the remote network CIDR blocks: [10.0.0.0/24]",
		},
		{
			name: "node ip not in DNS and found via ResolveBindAddress within remote node network",
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:   "test-cluster",
						Region: "us-west-2",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:        "nonexistent.node",
							TrustAnchorARN:  "arn:aws:rolesanywhere:us-west-2:123456789012:trust-anchor/abcdef12-3456-7890-abcd-ef1234567890",
							ProfileARN:      "arn:aws:rolesanywhere:us-west-2:123456789012:profile/fedcba98-7654-3210-fedc-ba9876543210",
							RoleARN:         "arn:aws:iam::123456789012:role/EKSHybridNodeRole",
							CertificatePath: "/etc/eks/certs/node-cert.pem",
							PrivateKeyPath:  "/etc/eks/certs/node-key.pem",
						},
					},
				},
				Status: api.NodeConfigStatus{
					Hybrid: api.HybridDetails{
						NodeName: "nonexistent.node",
					},
				},
			},
			cluster: &eks.Cluster{
				RemoteNetworkConfig: &eks.RemoteNetworkConfig{
					RemoteNodeNetworks: []*eks.RemoteNodeNetwork{
						{CIDRs: []*string{aws.String("192.168.0.0/24")}},
					},
				},
			},
			networkUtils: &MockIPValidationNetworkUtils{
				DNSRecords:       map[string][]net.IP{},
				ResolvedBindAddr: net.ParseIP("192.168.0.10"),
				BindAddrErr:      nil,
				NetworkInterfaces: []net.Addr{
					&net.IPNet{
						IP:   net.ParseIP("192.168.0.10"),
						Mask: net.CIDRMask(24, 32),
					},
				},
			},
			expectedErr: "",
		},
		{
			name: "node ip found via ResolveBindAddress outside of remote node network",
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:   "test-cluster",
						Region: "us-west-2",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:        "nonexistent.node",
							TrustAnchorARN:  "arn:aws:rolesanywhere:us-west-2:123456789012:trust-anchor/abcdef12-3456-7890-abcd-ef1234567890",
							ProfileARN:      "arn:aws:rolesanywhere:us-west-2:123456789012:profile/fedcba98-7654-3210-fedc-ba9876543210",
							RoleARN:         "arn:aws:iam::123456789012:role/EKSHybridNodeRole",
							CertificatePath: "/etc/eks/certs/node-cert.pem",
							PrivateKeyPath:  "/etc/eks/certs/node-key.pem",
						},
					},
				},
				Status: api.NodeConfigStatus{
					Hybrid: api.HybridDetails{
						NodeName: "nonexistent.node",
					},
				},
			},
			cluster: &eks.Cluster{
				RemoteNetworkConfig: &eks.RemoteNetworkConfig{
					RemoteNodeNetworks: []*eks.RemoteNodeNetwork{
						{CIDRs: []*string{aws.String("10.0.0.0/24")}},
					},
				},
			},
			networkUtils: &MockIPValidationNetworkUtils{
				DNSRecords:       map[string][]net.IP{},
				ResolvedBindAddr: net.ParseIP("192.168.1.1"),
				BindAddrErr:      nil,
				NetworkInterfaces: []net.Addr{
					&net.IPNet{
						IP:   net.ParseIP("192.168.1.1"),
						Mask: net.CIDRMask(16, 32),
					},
					&net.IPNet{
						IP:   net.ParseIP("10.0.0.1"),
						Mask: net.CIDRMask(16, 32),
					},
				},
			},
			expectedErr: "node IP 192.168.1.1 is not in any of the remote network CIDR blocks: [10.0.0.0/24]",
		},
		{
			name: "Both DNS and ResolveBindAddress fail",
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name:   "test-cluster",
						Region: "us-west-2",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName:        "nonexistent.node",
							TrustAnchorARN:  "arn:aws:rolesanywhere:us-west-2:123456789012:trust-anchor/abcdef12-3456-7890-abcd-ef1234567890",
							ProfileARN:      "arn:aws:rolesanywhere:us-west-2:123456789012:profile/fedcba98-7654-3210-fedc-ba9876543210",
							RoleARN:         "arn:aws:iam::123456789012:role/EKSHybridNodeRole",
							CertificatePath: "/etc/eks/certs/node-cert.pem",
							PrivateKeyPath:  "/etc/eks/certs/node-key.pem",
						},
					},
				},
				Status: api.NodeConfigStatus{
					Hybrid: api.HybridDetails{
						NodeName: "nonexistent.node",
					},
				},
			},
			cluster: &eks.Cluster{
				RemoteNetworkConfig: &eks.RemoteNetworkConfig{
					RemoteNodeNetworks: []*eks.RemoteNodeNetwork{
						{CIDRs: []*string{aws.String("10.0.0.0/24")}},
					},
				},
			},
			networkUtils: &MockIPValidationNetworkUtils{
				DNSRecords:        map[string][]net.IP{},
				ResolvedBindAddr:  nil,
				BindAddrErr:       fmt.Errorf("failed to resolve bind address"),
				NetworkInterfaces: []net.Addr{},
			},
			expectedErr: "couldn't get ip address of node",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			hnp, err := hybrid.NewHybridNodeProvider(
				tt.nodeConfig,
				[]string{},
				zap.NewNop(),
				hybrid.WithCluster(tt.cluster),
				hybrid.WithNetworkUtils(tt.networkUtils),
			)
			g.Expect(err).To(Succeed())

			err = hnp.Validate()

			if tt.expectedErr != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.expectedErr))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}

			// Check that all cases pass when node-ip-validation is skipped
			hnp, err = hybrid.NewHybridNodeProvider(
				tt.nodeConfig,
				[]string{"node-ip-validation"},
				zap.NewNop(),
				hybrid.WithCluster(tt.cluster),
				hybrid.WithNetworkUtils(tt.networkUtils),
			)
			g.Expect(err).To(Succeed())
			err = hnp.Validate()
			g.Expect(err).NotTo(HaveOccurred())
		})
	}
}

// MockIPValidationNetworkUtils mocks the network utilities for testing
type MockIPValidationNetworkUtils struct {
	// For DNS resolution
	DNSRecords map[string][]net.IP

	// For address resolution
	ResolvedBindAddr net.IP
	BindAddrErr      error

	// For interface addresses
	NetworkInterfaces []net.Addr
	InterfacesErr     error
}

func (m *MockIPValidationNetworkUtils) LookupIP(host string) ([]net.IP, error) {
	if ips, exists := m.DNSRecords[host]; exists {
		return ips, nil
	}
	return nil, &net.DNSError{
		Err:        "no such host",
		Name:       host,
		IsNotFound: true,
	}
}

func (m *MockIPValidationNetworkUtils) ResolveBindAddress(bindAddress net.IP) (net.IP, error) {
	return m.ResolvedBindAddr, m.BindAddrErr
}

func (m *MockIPValidationNetworkUtils) InterfaceAddrs() ([]net.Addr, error) {
	return m.NetworkInterfaces, m.InterfacesErr
}
