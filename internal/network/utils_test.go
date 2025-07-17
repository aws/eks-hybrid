package network

import (
	"fmt"
	"net"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	. "github.com/onsi/gomega"
)

func TestContainsIP(t *testing.T) {
	tests := []struct {
		name     string
		cidr     string
		ip       net.IP
		expected bool
		wantErr  bool
	}{
		{
			name:     "IP within CIDR",
			cidr:     "10.0.0.0/24",
			ip:       net.ParseIP("10.0.0.5"),
			expected: true,
			wantErr:  false,
		},
		{
			name:     "IP outside CIDR",
			cidr:     "10.0.0.0/24",
			ip:       net.ParseIP("192.168.1.1"),
			expected: false,
			wantErr:  false,
		},
		{
			name:     "IP at network boundary",
			cidr:     "10.0.0.0/24",
			ip:       net.ParseIP("10.0.0.0"),
			expected: true,
			wantErr:  false,
		},
		{
			name:     "IP at broadcast boundary",
			cidr:     "10.0.0.0/24",
			ip:       net.ParseIP("10.0.0.255"),
			expected: true,
			wantErr:  false,
		},
		{
			name:     "Invalid CIDR",
			cidr:     "invalid-cidr",
			ip:       net.ParseIP("10.0.0.1"),
			expected: false,
			wantErr:  true,
		},
		{
			name:     "IPv6 CIDR with IPv4 IP",
			cidr:     "2001:db8::/32",
			ip:       net.ParseIP("10.0.0.1"),
			expected: false,
			wantErr:  false,
		},
		{
			name:     "IPv6 CIDR with IPv6 IP - match",
			cidr:     "2001:db8::/32",
			ip:       net.ParseIP("2001:db8::1"),
			expected: true,
			wantErr:  false,
		},
		{
			name:     "IPv6 CIDR with IPv6 IP - no match",
			cidr:     "2001:db8::/32",
			ip:       net.ParseIP("2001:db9::1"),
			expected: false,
			wantErr:  false,
		},
		{
			name:     "Single host CIDR /32",
			cidr:     "10.0.0.1/32",
			ip:       net.ParseIP("10.0.0.1"),
			expected: true,
			wantErr:  false,
		},
		{
			name:     "Single host CIDR /32 - no match",
			cidr:     "10.0.0.1/32",
			ip:       net.ParseIP("10.0.0.2"),
			expected: false,
			wantErr:  false,
		},
		{
			name:     "Large subnet /8",
			cidr:     "10.0.0.0/8",
			ip:       net.ParseIP("10.255.255.255"),
			expected: true,
			wantErr:  false,
		},
		{
			name:     "Nil IP",
			cidr:     "10.0.0.0/24",
			ip:       nil,
			expected: false,
			wantErr:  false,
		},
		{
			name:     "Empty CIDR",
			cidr:     "",
			ip:       net.ParseIP("10.0.0.1"),
			expected: false,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			result, err := ContainsIP(tt.cidr, tt.ip)

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(result).To(Equal(tt.expected))
			}
		})
	}
}

func TestIsIPInCIDRs(t *testing.T) {
	tests := []struct {
		name     string
		ip       net.IP
		cidrs    []string
		expected bool
		wantErr  bool
	}{
		{
			name:     "IP in first CIDR",
			ip:       net.ParseIP("10.0.0.5"),
			cidrs:    []string{"10.0.0.0/24", "192.168.1.0/24"},
			expected: true,
			wantErr:  false,
		},
		{
			name:     "IP in second CIDR",
			ip:       net.ParseIP("192.168.1.10"),
			cidrs:    []string{"10.0.0.0/24", "192.168.1.0/24"},
			expected: true,
			wantErr:  false,
		},
		{
			name:     "IP not in any CIDR",
			ip:       net.ParseIP("172.16.0.1"),
			cidrs:    []string{"10.0.0.0/24", "192.168.1.0/24"},
			expected: false,
			wantErr:  false,
		},
		{
			name:     "Empty CIDR list",
			ip:       net.ParseIP("10.0.0.1"),
			cidrs:    []string{},
			expected: false,
			wantErr:  false,
		},
		{
			name:     "Invalid IP (IPv6)",
			ip:       net.ParseIP("2001:db8::1"),
			cidrs:    []string{"10.0.0.0/24"},
			expected: false,
			wantErr:  true,
		},
		{
			name:     "Invalid CIDR in list",
			ip:       net.ParseIP("10.0.0.1"),
			cidrs:    []string{"invalid-cidr", "10.0.0.0/24"},
			expected: false,
			wantErr:  true,
		},
		{
			name:     "Nil IP",
			ip:       nil,
			cidrs:    []string{"10.0.0.0/24"},
			expected: false,
			wantErr:  true,
		},
		{
			name:     "Single CIDR match",
			ip:       net.ParseIP("10.0.0.1"),
			cidrs:    []string{"10.0.0.0/24"},
			expected: true,
			wantErr:  false,
		},
		{
			name:     "Multiple CIDRs no match",
			ip:       net.ParseIP("172.16.0.1"),
			cidrs:    []string{"10.0.0.0/24", "192.168.1.0/24", "172.17.0.0/16"},
			expected: false,
			wantErr:  false,
		},
		{
			name:     "IP at CIDR boundary",
			ip:       net.ParseIP("10.0.0.0"),
			cidrs:    []string{"10.0.0.0/24"},
			expected: true,
			wantErr:  false,
		},
		{
			name:     "IP at CIDR broadcast",
			ip:       net.ParseIP("10.0.0.255"),
			cidrs:    []string{"10.0.0.0/24"},
			expected: true,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			result, err := IsIPInCIDRs(tt.ip, tt.cidrs)

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(result).To(Equal(tt.expected))
			}
		})
	}
}

func TestExtractCIDRsFromNodeNetworks(t *testing.T) {
	tests := []struct {
		name     string
		networks []types.RemoteNodeNetwork
		expected []string
	}{
		{
			name: "Single network with multiple CIDRs",
			networks: []types.RemoteNodeNetwork{
				{
					Cidrs: []string{"10.0.0.0/24", "192.168.1.0/24"},
				},
			},
			expected: []string{"10.0.0.0/24", "192.168.1.0/24"},
		},
		{
			name: "Multiple networks with CIDRs",
			networks: []types.RemoteNodeNetwork{
				{
					Cidrs: []string{"10.0.0.0/24"},
				},
				{
					Cidrs: []string{"192.168.1.0/24", "172.16.0.0/16"},
				},
			},
			expected: []string{"10.0.0.0/24", "192.168.1.0/24", "172.16.0.0/16"},
		},
		{
			name: "Network with empty CIDR",
			networks: []types.RemoteNodeNetwork{
				{
					Cidrs: []string{"10.0.0.0/24", "", "192.168.1.0/24"},
				},
			},
			expected: []string{"10.0.0.0/24", "192.168.1.0/24"},
		},
		{
			name:     "Empty networks",
			networks: []types.RemoteNodeNetwork{},
			expected: []string{},
		},
		{
			name: "Network with no CIDRs",
			networks: []types.RemoteNodeNetwork{
				{
					Cidrs: []string{},
				},
			},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			result := ExtractCIDRsFromNodeNetworks(tt.networks)
			g.Expect(result).To(Equal(tt.expected))
		})
	}
}

func TestExtractFlagValue(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		flag     string
		expected string
	}{
		{
			name:     "Flag present",
			args:     []string{"--node-ip=10.0.0.1", "--other-flag=value"},
			flag:     "node-ip",
			expected: "10.0.0.1",
		},
		{
			name:     "Flag not present",
			args:     []string{"--other-flag=value"},
			flag:     "node-ip",
			expected: "",
		},
		{
			name:     "Flag present multiple times - returns last",
			args:     []string{"--node-ip=10.0.0.1", "--node-ip=10.0.0.2"},
			flag:     "node-ip",
			expected: "10.0.0.2",
		},
		{
			name:     "Empty args",
			args:     []string{},
			flag:     "node-ip",
			expected: "",
		},
		{
			name:     "Flag with empty value",
			args:     []string{"--node-ip="},
			flag:     "node-ip",
			expected: "",
		},
		{
			name:     "Flag with complex value",
			args:     []string{"--config-file=/path/to/config.yaml"},
			flag:     "config-file",
			expected: "/path/to/config.yaml",
		},
		{
			name:     "Flag with spaces in value",
			args:     []string{"--description=test with spaces"},
			flag:     "description",
			expected: "test with spaces",
		},
		{
			name:     "Flag with special characters",
			args:     []string{"--password=p@ssw0rd!"},
			flag:     "password",
			expected: "p@ssw0rd!",
		},
		{
			name:     "Flag name case sensitive",
			args:     []string{"--Node-IP=10.0.0.1"},
			flag:     "node-ip",
			expected: "",
		},
		{
			name:     "Flag with equals in value",
			args:     []string{"--env=KEY=VALUE"},
			flag:     "env",
			expected: "KEY=VALUE",
		},
		{
			name:     "Flag prefix match but not exact",
			args:     []string{"--node-ip-range=10.0.0.0/24"},
			flag:     "node-ip",
			expected: "",
		},
		{
			name:     "Empty flag name",
			args:     []string{"--node-ip=10.0.0.1"},
			flag:     "",
			expected: "",
		},
		{
			name:     "Flag without equals sign",
			args:     []string{"--verbose", "--node-ip=10.0.0.1"},
			flag:     "verbose",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			result := ExtractFlagValue(tt.args, tt.flag)
			g.Expect(result).To(Equal(tt.expected))
		})
	}
}

func TestExtractNodeIPFromFlags(t *testing.T) {
	tests := []struct {
		name        string
		kubeletArgs []string
		expected    net.IP
		wantErr     bool
		errContains string
	}{
		{
			name:        "Valid IPv4",
			kubeletArgs: []string{"--node-ip=10.0.0.1"},
			expected:    net.ParseIP("10.0.0.1"),
			wantErr:     false,
		},
		{
			name:        "No node-ip flag",
			kubeletArgs: []string{"--other-flag=value"},
			expected:    nil,
			wantErr:     false,
		},
		{
			name:        "Invalid IP",
			kubeletArgs: []string{"--node-ip=invalid-ip"},
			expected:    nil,
			wantErr:     true,
			errContains: "invalid ip invalid-ip in --node-ip flag",
		},
		{
			name:        "IPv6 address",
			kubeletArgs: []string{"--node-ip=2001:db8::1"},
			expected:    nil,
			wantErr:     true,
			errContains: "invalid IPv6 address 2001:db8::1 in --node-ip flag",
		},
		{
			name:        "Empty kubelet args",
			kubeletArgs: []string{},
			expected:    nil,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			result, err := ExtractNodeIPFromFlags(tt.kubeletArgs)

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.errContains))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				if tt.expected != nil {
					g.Expect(result.Equal(tt.expected)).To(BeTrue())
				} else {
					g.Expect(result).To(BeNil())
				}
			}
		})
	}
}

func TestValidateNodeIP(t *testing.T) {
	tests := []struct {
		name        string
		nodeIP      net.IP
		network     Network
		wantErr     bool
		errContains string
	}{
		{
			name:   "Valid IPv4 found in interfaces",
			nodeIP: net.ParseIP("10.0.0.1"),
			network: &mockNetwork{
				NetworkInterfaces: []net.Addr{
					&net.IPNet{IP: net.ParseIP("10.0.0.1"), Mask: net.CIDRMask(24, 32)},
					&net.IPNet{IP: net.ParseIP("192.168.1.1"), Mask: net.CIDRMask(24, 32)},
				},
			},
			wantErr: false,
		},
		{
			name:   "IP not found in interfaces",
			nodeIP: net.ParseIP("172.16.0.1"),
			network: &mockNetwork{
				NetworkInterfaces: []net.Addr{
					&net.IPNet{IP: net.ParseIP("10.0.0.1"), Mask: net.CIDRMask(24, 32)},
				},
			},
			wantErr:     true,
			errContains: "not found in the host's network interfaces",
		},
		{
			name:        "Loopback IP",
			nodeIP:      net.ParseIP("127.0.0.1"),
			network:     &mockNetwork{},
			wantErr:     true,
			errContains: "nodeIP can't be loopback address",
		},
		{
			name:        "Multicast IP",
			nodeIP:      net.ParseIP("224.0.0.1"),
			network:     &mockNetwork{},
			wantErr:     true,
			errContains: "nodeIP can't be a multicast address",
		},
		{
			name:        "Unspecified IP",
			nodeIP:      net.ParseIP("0.0.0.0"),
			network:     &mockNetwork{},
			wantErr:     true,
			errContains: "nodeIP can't be an all zeros address",
		},
		{
			name:   "Interface error",
			nodeIP: net.ParseIP("10.0.0.1"),
			network: &mockNetwork{
				InterfacesErr: fmt.Errorf("interface error"),
			},
			wantErr:     true,
			errContains: "interface error",
		},
		{
			name:        "Link-local unicast IP",
			nodeIP:      net.ParseIP("169.254.1.1"),
			network:     &mockNetwork{},
			wantErr:     true,
			errContains: "nodeIP can't be a link-local unicast address",
		},
		{
			name:   "Valid IPv6 found in interfaces",
			nodeIP: net.ParseIP("2001:db8::1"),
			network: &mockNetwork{
				NetworkInterfaces: []net.Addr{
					&net.IPNet{IP: net.ParseIP("2001:db8::1"), Mask: net.CIDRMask(64, 128)},
				},
			},
			wantErr: false,
		},
		{
			name:        "IPv6 loopback",
			nodeIP:      net.ParseIP("::1"),
			network:     &mockNetwork{},
			wantErr:     true,
			errContains: "nodeIP can't be loopback address",
		},
		{
			name:        "IPv6 unspecified",
			nodeIP:      net.ParseIP("::"),
			network:     &mockNetwork{},
			wantErr:     true,
			errContains: "nodeIP can't be an all zeros address",
		},
		{
			name:        "IPv6 multicast",
			nodeIP:      net.ParseIP("ff02::1"),
			network:     &mockNetwork{},
			wantErr:     true,
			errContains: "nodeIP can't be a multicast address",
		},
		{
			name:        "IPv6 link-local",
			nodeIP:      net.ParseIP("fe80::1"),
			network:     &mockNetwork{},
			wantErr:     true,
			errContains: "nodeIP can't be a link-local unicast address",
		},
		{
			name:   "Valid IP with IPAddr interface",
			nodeIP: net.ParseIP("10.0.0.1"),
			network: &mockNetwork{
				NetworkInterfaces: []net.Addr{
					&net.IPAddr{IP: net.ParseIP("10.0.0.1")},
				},
			},
			wantErr: false,
		},
		{
			name:        "Nil IP",
			nodeIP:      nil,
			network:     &mockNetwork{},
			wantErr:     true,
			errContains: "nodeIP must be a valid IP address",
		},
		{
			name:   "Empty interfaces list",
			nodeIP: net.ParseIP("10.0.0.1"),
			network: &mockNetwork{
				NetworkInterfaces: []net.Addr{},
			},
			wantErr:     true,
			errContains: "not found in the host's network interfaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			err := ValidateNodeIP(tt.nodeIP, tt.network)

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.errContains))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestGetNodeIP(t *testing.T) {
	tests := []struct {
		name        string
		kubeletArgs []string
		nodeName    string
		network     Network
		expected    net.IP
		wantErr     bool
		errContains string
	}{
		{
			name:        "Node IP from flags",
			kubeletArgs: []string{"--node-ip=10.0.0.1"},
			nodeName:    "",
			network: &mockNetwork{
				NetworkInterfaces: []net.Addr{
					&net.IPNet{IP: net.ParseIP("10.0.0.1"), Mask: net.CIDRMask(24, 32)},
				},
			},
			expected: net.ParseIP("10.0.0.1"),
			wantErr:  false,
		},
		{
			name:        "Node IP from DNS resolution",
			kubeletArgs: []string{},
			nodeName:    "test-node",
			network: &mockNetwork{
				DNSRecords: map[string][]net.IP{
					"test-node": {net.ParseIP("10.0.0.2")},
				},
				NetworkInterfaces: []net.Addr{
					&net.IPNet{IP: net.ParseIP("10.0.0.2"), Mask: net.CIDRMask(24, 32)},
				},
			},
			expected: net.ParseIP("10.0.0.2"),
			wantErr:  false,
		},
		{
			name:        "Node IP from ResolveBindAddress",
			kubeletArgs: []string{},
			nodeName:    "",
			network: &mockNetwork{
				ResolvedBindAddr: net.ParseIP("10.0.0.3"),
				NetworkInterfaces: []net.Addr{
					&net.IPNet{IP: net.ParseIP("10.0.0.3"), Mask: net.CIDRMask(24, 32)},
				},
			},
			expected: net.ParseIP("10.0.0.3"),
			wantErr:  false,
		},
		{
			name:        "All methods fail",
			kubeletArgs: []string{},
			nodeName:    "",
			network: &mockNetwork{
				BindAddrErr: fmt.Errorf("failed to resolve"),
			},
			expected:    nil,
			wantErr:     true,
			errContains: "couldn't get ip address of node",
		},
		{
			name:        "Invalid node IP flag",
			kubeletArgs: []string{"--node-ip=invalid"},
			nodeName:    "",
			network:     &mockNetwork{},
			expected:    nil,
			wantErr:     true,
			errContains: "invalid ip invalid in --node-ip flag",
		},
		{
			name:        "Node IP flag with unspecified IP (0.0.0.0)",
			kubeletArgs: []string{"--node-ip=0.0.0.0"},
			nodeName:    "",
			network: &mockNetwork{
				ResolvedBindAddr: net.ParseIP("10.0.0.4"),
				NetworkInterfaces: []net.Addr{
					&net.IPNet{IP: net.ParseIP("10.0.0.4"), Mask: net.CIDRMask(24, 32)},
				},
			},
			expected: net.ParseIP("10.0.0.4"),
			wantErr:  false,
		},
		{
			name:        "DNS resolution with multiple IPs - first valid",
			kubeletArgs: []string{},
			nodeName:    "test-node",
			network: &mockNetwork{
				DNSRecords: map[string][]net.IP{
					"test-node": {net.ParseIP("127.0.0.1"), net.ParseIP("10.0.0.5")}, // loopback first, valid second
				},
				NetworkInterfaces: []net.Addr{
					&net.IPNet{IP: net.ParseIP("10.0.0.5"), Mask: net.CIDRMask(24, 32)},
				},
			},
			expected: net.ParseIP("10.0.0.5"),
			wantErr:  false,
		},
		{
			name:        "DNS resolution with IPv6 IP - should skip",
			kubeletArgs: []string{},
			nodeName:    "test-node",
			network: &mockNetwork{
				DNSRecords: map[string][]net.IP{
					"test-node": {net.ParseIP("2001:db8::1")}, // IPv6 should be skipped
				},
				ResolvedBindAddr: net.ParseIP("10.0.0.6"),
				NetworkInterfaces: []net.Addr{
					&net.IPNet{IP: net.ParseIP("10.0.0.6"), Mask: net.CIDRMask(24, 32)},
				},
			},
			expected: net.ParseIP("10.0.0.6"),
			wantErr:  false,
		},
		{
			name:        "DNS resolution with invalid IP - should skip",
			kubeletArgs: []string{},
			nodeName:    "test-node",
			network: &mockNetwork{
				DNSRecords: map[string][]net.IP{
					"test-node": {net.ParseIP("224.0.0.1")}, // multicast should be skipped
				},
				ResolvedBindAddr: net.ParseIP("10.0.0.7"),
				NetworkInterfaces: []net.Addr{
					&net.IPNet{IP: net.ParseIP("10.0.0.7"), Mask: net.CIDRMask(24, 32)},
				},
			},
			expected: net.ParseIP("10.0.0.7"),
			wantErr:  false,
		},
		{
			name:        "DNS resolution fails, fallback to ResolveBindAddress",
			kubeletArgs: []string{},
			nodeName:    "nonexistent-node",
			network: &mockNetwork{
				DNSRecords:       map[string][]net.IP{}, // Empty, will cause DNS error
				ResolvedBindAddr: net.ParseIP("10.0.0.8"),
				NetworkInterfaces: []net.Addr{
					&net.IPNet{IP: net.ParseIP("10.0.0.8"), Mask: net.CIDRMask(24, 32)},
				},
			},
			expected: net.ParseIP("10.0.0.8"),
			wantErr:  false,
		},
		{
			name:        "Empty node name, use ResolveBindAddress",
			kubeletArgs: []string{},
			nodeName:    "",
			network: &mockNetwork{
				ResolvedBindAddr: net.ParseIP("10.0.0.9"),
				NetworkInterfaces: []net.Addr{
					&net.IPNet{IP: net.ParseIP("10.0.0.9"), Mask: net.CIDRMask(24, 32)},
				},
			},
			expected: net.ParseIP("10.0.0.9"),
			wantErr:  false,
		},
		{
			name:        "ResolveBindAddress returns nil",
			kubeletArgs: []string{},
			nodeName:    "",
			network: &mockNetwork{
				ResolvedBindAddr: nil,
				BindAddrErr:      nil,
			},
			expected:    nil,
			wantErr:     true,
			errContains: "couldn't get ip address of node",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			result, err := GetNodeIP(tt.kubeletArgs, tt.nodeName, tt.network)

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.errContains))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(result.Equal(tt.expected)).To(BeTrue())
			}
		})
	}
}

func TestValidateIPInRemoteNodeNetwork(t *testing.T) {
	tests := []struct {
		name              string
		ipAddr            net.IP
		remoteNodeNetwork []types.RemoteNodeNetwork
		wantErr           bool
		errContains       string
	}{
		{
			name:   "IP in remote network",
			ipAddr: net.ParseIP("10.0.0.1"),
			remoteNodeNetwork: []types.RemoteNodeNetwork{
				{Cidrs: []string{"10.0.0.0/24"}},
			},
			wantErr: false,
		},
		{
			name:   "IP not in remote network",
			ipAddr: net.ParseIP("192.168.1.1"),
			remoteNodeNetwork: []types.RemoteNodeNetwork{
				{Cidrs: []string{"10.0.0.0/24"}},
			},
			wantErr:     true,
			errContains: "node IP 192.168.1.1 is not in any of the remote network CIDR blocks",
		},
		{
			name:   "IP in one of multiple networks",
			ipAddr: net.ParseIP("192.168.1.1"),
			remoteNodeNetwork: []types.RemoteNodeNetwork{
				{Cidrs: []string{"10.0.0.0/24", "192.168.1.0/24"}},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			err := ValidateIPInRemoteNodeNetwork(tt.ipAddr, tt.remoteNodeNetwork)

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.errContains))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestValidateClusterRemoteNetworkConfig(t *testing.T) {
	tests := []struct {
		name        string
		cluster     *types.Cluster
		wantErr     bool
		errContains string
	}{
		{
			name: "Valid remote network config",
			cluster: &types.Cluster{
				Name: &[]string{"test-cluster"}[0],
				RemoteNetworkConfig: &types.RemoteNetworkConfigResponse{
					RemoteNodeNetworks: []types.RemoteNodeNetwork{
						{Cidrs: []string{"10.0.0.0/24"}},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "No remote network config",
			cluster: &types.Cluster{
				Name:                &[]string{"test-cluster"}[0],
				RemoteNetworkConfig: nil,
			},
			wantErr:     true,
			errContains: "remote network config is not set for cluster test-cluster",
		},
		{
			name: "No remote node networks",
			cluster: &types.Cluster{
				Name: &[]string{"test-cluster"}[0],
				RemoteNetworkConfig: &types.RemoteNetworkConfigResponse{
					RemoteNodeNetworks: nil,
				},
			},
			wantErr:     true,
			errContains: "remote node networks not found in remote network config for cluster test-cluster",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			err := ValidateClusterRemoteNetworkConfig(tt.cluster)

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.errContains))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestDefaultKubeletNetwork(t *testing.T) {
	g := NewWithT(t)

	network := NewDefaultNetwork()
	g.Expect(network).NotTo(BeNil())

	// Test that it implements the Network interface
	_ = network

	// Test LookupIP with localhost (should work on most systems)
	ips, err := network.LookupIP("localhost")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(ips)).To(BeNumerically(">", 0))

	// Test InterfaceAddrs
	addrs, err := network.InterfaceAddrs()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(addrs)).To(BeNumerically(">", 0))

	// Test ResolveBindAddress with nil (should return a valid IP)
	ip, err := network.ResolveBindAddress(nil)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(ip).NotTo(BeNil())
}

// mockNetwork implements the Network interface for testing
type mockNetwork struct {
	DNSRecords        map[string][]net.IP
	ResolvedBindAddr  net.IP
	BindAddrErr       error
	NetworkInterfaces []net.Addr
	InterfacesErr     error
}

func (m *mockNetwork) LookupIP(host string) ([]net.IP, error) {
	if ips, exists := m.DNSRecords[host]; exists {
		return ips, nil
	}
	return nil, &net.DNSError{
		Err:        "no such host",
		Name:       host,
		IsNotFound: true,
	}
}

func (m *mockNetwork) ResolveBindAddress(bindAddress net.IP) (net.IP, error) {
	return m.ResolvedBindAddr, m.BindAddrErr
}

func (m *mockNetwork) InterfaceAddrs() ([]net.Addr, error) {
	return m.NetworkInterfaces, m.InterfacesErr
}

func TestValidateMTU(t *testing.T) {
	tests := []struct {
		name        string
		mtu         int
		expectError bool
		errContains string
	}{
		{
			name:        "Valid standard MTU - minimum",
			mtu:         68,
			expectError: false,
		},
		{
			name:        "Valid standard MTU - typical",
			mtu:         1500,
			expectError: false,
		},
		{
			name:        "Valid standard MTU - mid-range",
			mtu:         1000,
			expectError: false,
		},
		{
			name:        "Valid jumbo frame MTU - minimum",
			mtu:         8000,
			expectError: false,
		},
		{
			name:        "Valid jumbo frame MTU - maximum",
			mtu:         9001,
			expectError: false,
		},
		{
			name:        "Valid jumbo frame MTU - mid-range",
			mtu:         8500,
			expectError: false,
		},
		{
			name:        "Invalid MTU - zero",
			mtu:         0,
			expectError: true,
			errContains: "MTU must be a positive value",
		},
		{
			name:        "Invalid MTU - negative",
			mtu:         -100,
			expectError: true,
			errContains: "MTU must be a positive value",
		},
		{
			name:        "Invalid MTU - too small",
			mtu:         67,
			expectError: true,
			errContains: "MTU 67 is not in acceptable ranges",
		},
		{
			name:        "Invalid MTU - gap between standard and jumbo",
			mtu:         1501,
			expectError: true,
			errContains: "MTU 1501 is not in acceptable ranges",
		},
		{
			name:        "Invalid MTU - gap between standard and jumbo (high)",
			mtu:         7999,
			expectError: true,
			errContains: "MTU 7999 is not in acceptable ranges",
		},
		{
			name:        "Invalid MTU - too large",
			mtu:         9002,
			expectError: true,
			errContains: "MTU 9002 is not in acceptable ranges",
		},
		{
			name:        "Invalid MTU - extremely large",
			mtu:         65536,
			expectError: true,
			errContains: "MTU 65536 is not in acceptable ranges",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			err := ValidateMTU(tt.mtu)

			if tt.expectError {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.errContains))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestFindNetworkInterfaceForIP(t *testing.T) {
	g := NewWithT(t)

	// Test with non-existent IP
	nonExistentIP := net.ParseIP("192.168.255.254")
	iface, err := FindNetworkInterfaceForIP(nonExistentIP)
	g.Expect(err).To(HaveOccurred())
	g.Expect(iface).To(BeNil())
	g.Expect(err.Error()).To(ContainSubstring("no active network interface found with IP"))
	g.Expect(err.Error()).To(ContainSubstring("192.168.255.254"))

	// Note: We don't test with localhost IP here because loopback interfaces
	// are skipped by FindNetworkInterfaceForIP (by design for MTU validation)
}

func TestValidateNetworkInterfaceMTUForIP(t *testing.T) {
	g := NewWithT(t)

	// Test with non-existent IP
	nonExistentIP := net.ParseIP("192.168.255.254")
	err := ValidateNetworkInterfaceMTUForIP(nonExistentIP)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("no active network interface found with IP"))
	g.Expect(err.Error()).To(ContainSubstring("192.168.255.254"))

	// Note: We don't test with localhost IP here because loopback interfaces
	// are skipped by design for MTU validation purposes
}

// Integration test for MTU validation with mock interfaces
func TestMTUValidationIntegration(t *testing.T) {
	tests := []struct {
		name            string
		testMTUValues   []int
		expectedValid   []int
		expectedInvalid []int
	}{
		{
			name:            "Mixed MTU values",
			testMTUValues:   []int{68, 1500, 1501, 8000, 9001, 9002},
			expectedValid:   []int{68, 1500, 8000, 9001},
			expectedInvalid: []int{1501, 9002},
		},
		{
			name:            "All standard MTU values",
			testMTUValues:   []int{68, 576, 1000, 1500},
			expectedValid:   []int{68, 576, 1000, 1500},
			expectedInvalid: []int{},
		},
		{
			name:            "All jumbo frame MTU values",
			testMTUValues:   []int{8000, 8500, 9000, 9001},
			expectedValid:   []int{8000, 8500, 9000, 9001},
			expectedInvalid: []int{},
		},
		{
			name:            "All invalid MTU values",
			testMTUValues:   []int{0, -1, 67, 1501, 7999, 9002},
			expectedValid:   []int{},
			expectedInvalid: []int{0, -1, 67, 1501, 7999, 9002},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			actualValid := make([]int, 0)
			actualInvalid := make([]int, 0)

			for _, mtu := range tt.testMTUValues {
				if ValidateMTU(mtu) == nil {
					actualValid = append(actualValid, mtu)
				} else {
					actualInvalid = append(actualInvalid, mtu)
				}
			}

			g.Expect(actualValid).To(Equal(tt.expectedValid))
			g.Expect(actualInvalid).To(Equal(tt.expectedInvalid))
		})
	}
}

// Test MTU validation edge cases
func TestMTUValidationEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		mtu         int
		expectError bool
		description string
	}{
		{
			name:        "Minimum IPv4 MTU",
			mtu:         68,
			expectError: false,
			description: "68 is the minimum MTU for IPv4",
		},
		{
			name:        "Just below minimum IPv4 MTU",
			mtu:         67,
			expectError: true,
			description: "67 is below the minimum IPv4 MTU",
		},
		{
			name:        "Standard Ethernet MTU",
			mtu:         1500,
			expectError: false,
			description: "1500 is the standard Ethernet MTU",
		},
		{
			name:        "Just above standard Ethernet MTU",
			mtu:         1501,
			expectError: true,
			description: "1501 is in the gap between standard and jumbo frames",
		},
		{
			name:        "Just below jumbo frame minimum",
			mtu:         7999,
			expectError: true,
			description: "7999 is just below the jumbo frame range",
		},
		{
			name:        "Jumbo frame minimum",
			mtu:         8000,
			expectError: false,
			description: "8000 is the minimum jumbo frame MTU",
		},
		{
			name:        "Jumbo frame maximum",
			mtu:         9001,
			expectError: false,
			description: "9001 is the maximum jumbo frame MTU",
		},
		{
			name:        "Just above jumbo frame maximum",
			mtu:         9002,
			expectError: true,
			description: "9002 is above the maximum jumbo frame MTU",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			err := ValidateMTU(tt.mtu)

			if tt.expectError {
				g.Expect(err).To(HaveOccurred(), tt.description)
			} else {
				g.Expect(err).NotTo(HaveOccurred(), tt.description)
			}
		})
	}
}
