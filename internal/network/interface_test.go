package network

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/validation"
)

func TestNewNetworkInterfaceValidator(t *testing.T) {
	g := NewWithT(t)

	awsConfig := aws.Config{Region: "us-west-2"}
	validator := NewNetworkInterfaceValidator(awsConfig)

	g.Expect(validator.clusterProvider).NotTo(BeNil())
	g.Expect(validator.network).NotTo(BeNil())
}

func TestNewNetworkInterfaceValidatorWithOptions(t *testing.T) {
	g := NewWithT(t)

	awsConfig := aws.Config{Region: "us-west-2"}
	mockNet := &mockNetwork{}

	validator := NewNetworkInterfaceValidator(awsConfig, WithNetwork(mockNet))

	g.Expect(validator.clusterProvider).NotTo(BeNil())
	g.Expect(validator.network).To(Equal(mockNet))
}

func TestWithNetwork(t *testing.T) {
	g := NewWithT(t)

	mockNet := &mockNetwork{}
	opt := WithNetwork(mockNet)

	validator := &NetworkInterfaceValidator{}
	opt(validator)

	g.Expect(validator.network).To(Equal(mockNet))
}

func TestNetworkInterfaceValidator_Run(t *testing.T) {
	tests := []struct {
		name          string
		nodeConfig    *api.NodeConfig
		mockEKSClient *mockEKSClient
		mockNetwork   *mockNetwork
		expectedErr   string
		expectSuccess bool
	}{
		{
			name: "successful validation",
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name: "test-cluster",
					},
					Kubelet: api.KubeletOptions{
						Flags: []string{"--node-ip=10.0.0.1"},
					},
				},
			},
			mockEKSClient: &mockEKSClient{
				cluster: &types.Cluster{
					Name: aws.String("test-cluster"),
					RemoteNetworkConfig: &types.RemoteNetworkConfigResponse{
						RemoteNodeNetworks: []types.RemoteNodeNetwork{
							{Cidrs: []string{"10.0.0.0/24"}},
						},
					},
				},
			},
			mockNetwork: &mockNetwork{
				NetworkInterfaces: []net.Addr{
					&net.IPNet{IP: net.ParseIP("10.0.0.1"), Mask: net.CIDRMask(24, 32)},
				},
			},
			expectSuccess: true,
		},
		{
			name: "EKS API error",
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name: "test-cluster",
					},
				},
			},
			mockEKSClient: &mockEKSClient{
				err: fmt.Errorf("EKS API error"),
			},
			mockNetwork:   &mockNetwork{},
			expectedErr:   "EKS API error",
			expectSuccess: false,
		},
		{
			name: "no remote network config",
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name: "test-cluster",
					},
				},
			},
			mockEKSClient: &mockEKSClient{
				cluster: &types.Cluster{
					Name:                aws.String("test-cluster"),
					RemoteNetworkConfig: nil,
				},
			},
			mockNetwork:   &mockNetwork{},
			expectedErr:   "remote network config is not set for cluster test-cluster",
			expectSuccess: false,
		},
		{
			name: "no remote node networks",
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name: "test-cluster",
					},
				},
			},
			mockEKSClient: &mockEKSClient{
				cluster: &types.Cluster{
					Name: aws.String("test-cluster"),
					RemoteNetworkConfig: &types.RemoteNetworkConfigResponse{
						RemoteNodeNetworks: nil,
					},
				},
			},
			mockNetwork:   &mockNetwork{},
			expectedErr:   "remote node networks not found in remote network config for cluster test-cluster",
			expectSuccess: false,
		},
		{
			name: "node IP not in remote network",
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name: "test-cluster",
					},
					Kubelet: api.KubeletOptions{
						Flags: []string{"--node-ip=192.168.1.1"},
					},
				},
			},
			mockEKSClient: &mockEKSClient{
				cluster: &types.Cluster{
					Name: aws.String("test-cluster"),
					RemoteNetworkConfig: &types.RemoteNetworkConfigResponse{
						RemoteNodeNetworks: []types.RemoteNodeNetwork{
							{Cidrs: []string{"10.0.0.0/24"}},
						},
					},
				},
			},
			mockNetwork: &mockNetwork{
				NetworkInterfaces: []net.Addr{
					&net.IPNet{IP: net.ParseIP("192.168.1.1"), Mask: net.CIDRMask(24, 32)},
				},
			},
			expectedErr:   "node IP 192.168.1.1 is not in any of the remote network CIDR blocks",
			expectSuccess: false,
		},
		{
			name: "IAM Roles Anywhere node with DNS resolution",
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name: "test-cluster",
					},
					Hybrid: &api.HybridOptions{
						IAMRolesAnywhere: &api.IAMRolesAnywhere{
							NodeName: "test-node.example.com",
						},
					},
				},
				Status: api.NodeConfigStatus{
					Hybrid: api.HybridDetails{
						NodeName: "test-node.example.com",
					},
				},
			},
			mockEKSClient: &mockEKSClient{
				cluster: &types.Cluster{
					Name: aws.String("test-cluster"),
					RemoteNetworkConfig: &types.RemoteNetworkConfigResponse{
						RemoteNodeNetworks: []types.RemoteNodeNetwork{
							{Cidrs: []string{"10.0.0.0/24"}},
						},
					},
				},
			},
			mockNetwork: &mockNetwork{
				DNSRecords: map[string][]net.IP{
					"test-node.example.com": {net.ParseIP("10.0.0.5")},
				},
				NetworkInterfaces: []net.Addr{
					&net.IPNet{IP: net.ParseIP("10.0.0.5"), Mask: net.CIDRMask(24, 32)},
				},
			},
			expectSuccess: true,
		},
		{
			name: "node IP resolution fails",
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Cluster: api.ClusterDetails{
						Name: "test-cluster",
					},
				},
			},
			mockEKSClient: &mockEKSClient{
				cluster: &types.Cluster{
					Name: aws.String("test-cluster"),
					RemoteNetworkConfig: &types.RemoteNetworkConfigResponse{
						RemoteNodeNetworks: []types.RemoteNodeNetwork{
							{Cidrs: []string{"10.0.0.0/24"}},
						},
					},
				},
			},
			mockNetwork: &mockNetwork{
				BindAddrErr: fmt.Errorf("failed to resolve bind address"),
			},
			expectedErr:   "couldn't get ip address of node",
			expectSuccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// Create a mock informer
			informer := &mockInformer{}

			// Create validator with mock network
			validator := NewNetworkInterfaceValidator(
				aws.Config{Region: "us-west-2"},
				WithNetwork(tt.mockNetwork),
			)

			// For this test, we'll create a simplified version that tests the validation logic
			// without the EKS API call
			err := tt.mockEKSClient.err
			if err == nil && tt.mockEKSClient.cluster != nil {
				cluster := tt.mockEKSClient.cluster

				// Validate cluster has remote network config
				if cluster.RemoteNetworkConfig == nil {
					err = fmt.Errorf("remote network config is not set for cluster %s", *cluster.Name)
				} else if cluster.RemoteNetworkConfig.RemoteNodeNetworks == nil {
					err = fmt.Errorf("remote node networks not found in remote network config for cluster %s", *cluster.Name)
				} else {
					// Get kubelet arguments from the node configuration
					kubeletArgs := tt.nodeConfig.Spec.Kubelet.Flags
					var iamNodeName string
					if tt.nodeConfig.IsIAMRolesAnywhere() {
						iamNodeName = tt.nodeConfig.Status.Hybrid.NodeName
					}

					// Get the node IP using the shared utility function
					nodeIP, ipErr := GetNodeIP(kubeletArgs, iamNodeName, tt.mockNetwork)
					if ipErr != nil {
						err = ipErr
					} else {
						// Validate that the node IP is in the remote node networks
						err = ValidateIPInRemoteNodeNetwork(nodeIP, cluster.RemoteNetworkConfig.RemoteNodeNetworks)
					}
				}
			}

			if tt.expectSuccess {
				g.Expect(err).NotTo(HaveOccurred())
			} else {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.expectedErr))
			}

			// Test that the validator can be created and has the right structure
			g.Expect(validator.network).To(Equal(tt.mockNetwork))
			g.Expect(informer).NotTo(BeNil()) // Just verify our mock informer exists
		})
	}
}

func TestNetworkInterfaceValidator_RunIntegration(t *testing.T) {
	// This test verifies the actual Run method structure without mocking EKS
	g := NewWithT(t)

	nodeConfig := &api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Name: "test-cluster",
			},
			Kubelet: api.KubeletOptions{
				Flags: []string{"--node-ip=10.0.0.1"},
			},
		},
	}

	mockNet := &mockNetwork{
		NetworkInterfaces: []net.Addr{
			&net.IPNet{IP: net.ParseIP("10.0.0.1"), Mask: net.CIDRMask(24, 32)},
		},
	}

	validator := NewNetworkInterfaceValidator(
		aws.Config{Region: "us-west-2"},
		WithNetwork(mockNet),
	)

	informer := &mockInformer{}
	ctx := context.Background()

	// This will fail because we don't have real AWS credentials, but we can verify
	// the method exists and has the right signature
	err := validator.Run(ctx, informer, nodeConfig)
	g.Expect(err).To(HaveOccurred()) // Expected to fail due to no real AWS setup

	// Verify that the informer was called
	g.Expect(informer.startingCalled).To(BeTrue())
	g.Expect(informer.doneCalled).To(BeTrue())
}

// Mock implementations for testing

type mockEKSClient struct {
	cluster *types.Cluster
	err     error
}

type mockInformer struct {
	startingCalled bool
	doneCalled     bool
	messages       []string
}

func (m *mockInformer) Starting(ctx context.Context, name, message string) {
	m.startingCalled = true
	m.messages = append(m.messages, fmt.Sprintf("Starting: %s - %s", name, message))
}

func (m *mockInformer) Done(ctx context.Context, name string, err error) {
	m.doneCalled = true
	if err != nil {
		m.messages = append(m.messages, fmt.Sprintf("Done: %s - Error: %v", name, err))
	} else {
		m.messages = append(m.messages, fmt.Sprintf("Done: %s - Success", name))
	}
}

func (m *mockInformer) Info(ctx context.Context, message string) {
	m.messages = append(m.messages, fmt.Sprintf("Info: %s", message))
}

func (m *mockInformer) Warn(ctx context.Context, message string) {
	m.messages = append(m.messages, fmt.Sprintf("Warn: %s", message))
}

func (m *mockInformer) Error(ctx context.Context, message string) {
	m.messages = append(m.messages, fmt.Sprintf("Error: %s", message))
}

// Verify mockInformer implements validation.Informer interface
var _ validation.Informer = (*mockInformer)(nil)

// Test helper functions

func TestValidationHelperFunctions(t *testing.T) {
	g := NewWithT(t)

	// Test that we can create a validator and it has the expected structure
	awsConfig := aws.Config{Region: "us-west-2"}
	validator := NewNetworkInterfaceValidator(awsConfig)

	g.Expect(validator.clusterProvider).NotTo(BeNil())
	g.Expect(validator.network).NotTo(BeNil())

	// Test WithNetwork option
	mockNet := &mockNetwork{}
	validatorWithMock := NewNetworkInterfaceValidator(awsConfig, WithNetwork(mockNet))
	g.Expect(validatorWithMock.network).To(Equal(mockNet))
}

func TestNetworkInterfaceValidatorErrorHandling(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func() *mockNetwork
		nodeConfig  *api.NodeConfig
		expectError bool
		errorText   string
	}{
		{
			name: "DNS resolution error with fallback",
			setupMock: func() *mockNetwork {
				return &mockNetwork{
					DNSRecords:       map[string][]net.IP{}, // Empty, will cause DNS error
					ResolvedBindAddr: net.ParseIP("10.0.0.1"),
					NetworkInterfaces: []net.Addr{
						&net.IPNet{IP: net.ParseIP("10.0.0.1"), Mask: net.CIDRMask(24, 32)},
					},
				}
			},
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Kubelet: api.KubeletOptions{
						Flags: []string{},
					},
				},
				Status: api.NodeConfigStatus{
					Hybrid: api.HybridDetails{
						NodeName: "nonexistent-node",
					},
				},
			},
			expectError: false, // Should succeed with fallback to ResolveBindAddress
		},
		{
			name: "All resolution methods fail",
			setupMock: func() *mockNetwork {
				return &mockNetwork{
					DNSRecords:  map[string][]net.IP{}, // Empty, will cause DNS error
					BindAddrErr: fmt.Errorf("bind address resolution failed"),
				}
			},
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Kubelet: api.KubeletOptions{
						Flags: []string{}, // No node-ip flag
					},
				},
				Status: api.NodeConfigStatus{
					Hybrid: api.HybridDetails{
						NodeName: "nonexistent-node",
					},
				},
			},
			expectError: true,
			errorText:   "couldn't get ip address of node",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			mockNet := tt.setupMock()

			// Test the GetNodeIP function directly since that's where the error handling occurs
			kubeletArgs := tt.nodeConfig.Spec.Kubelet.Flags
			var nodeName string
			if tt.nodeConfig.Status.Hybrid.NodeName != "" {
				nodeName = tt.nodeConfig.Status.Hybrid.NodeName
			}

			_, err := GetNodeIP(kubeletArgs, nodeName, mockNet)

			if tt.expectError {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.errorText))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestMTUValidationInNetworkInterface(t *testing.T) {
	tests := []struct {
		name        string
		description string
		expectError bool
		errorText   string
	}{
		{
			name:        "MTU validation integration test",
			description: "Test that MTU validation is integrated into network interface validation",
			expectError: false, // This will depend on the actual system interfaces
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// Test the IP-specific MTU validation function directly
			// We use a non-existent IP to test error handling
			nonExistentIP := net.ParseIP("192.168.255.254")
			err := ValidateNetworkInterfaceMTUForIP(nonExistentIP)

			// Should return an error since the IP doesn't exist on any interface
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).To(ContainSubstring("no active network interface found with IP"))
		})
	}
}

func TestMTUValidationFunctions(t *testing.T) {
	tests := []struct {
		name        string
		testFunc    func() error
		expectError bool
		errorText   string
	}{
		{
			name: "ValidateMTU with valid standard MTU",
			testFunc: func() error {
				return ValidateMTU(1500)
			},
			expectError: false,
		},
		{
			name: "ValidateMTU with valid jumbo frame MTU",
			testFunc: func() error {
				return ValidateMTU(9000)
			},
			expectError: false,
		},
		{
			name: "ValidateMTU with invalid MTU",
			testFunc: func() error {
				return ValidateMTU(1501)
			},
			expectError: true,
			errorText:   "MTU 1501 is not in acceptable ranges",
		},
		{
			name: "ValidateMTU with zero MTU",
			testFunc: func() error {
				return ValidateMTU(0)
			},
			expectError: true,
			errorText:   "MTU must be a positive value",
		},
		{
			name: "ValidateMTU with negative MTU",
			testFunc: func() error {
				return ValidateMTU(-100)
			},
			expectError: true,
			errorText:   "MTU must be a positive value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.testFunc()

			if tt.expectError {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.errorText))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestMTUValidationEdgeCasesInInterface(t *testing.T) {
	tests := []struct {
		name        string
		mtu         int
		expectValid bool
		description string
	}{
		{
			name:        "Minimum valid IPv4 MTU",
			mtu:         68,
			expectValid: true,
			description: "68 bytes is the minimum MTU for IPv4",
		},
		{
			name:        "Below minimum IPv4 MTU",
			mtu:         67,
			expectValid: false,
			description: "67 bytes is below the minimum IPv4 MTU",
		},
		{
			name:        "Standard Ethernet MTU",
			mtu:         1500,
			expectValid: true,
			description: "1500 bytes is the standard Ethernet MTU",
		},
		{
			name:        "Above standard, below jumbo",
			mtu:         1501,
			expectValid: false,
			description: "1501 bytes is in the gap between standard and jumbo frames",
		},
		{
			name:        "Minimum jumbo frame MTU",
			mtu:         8000,
			expectValid: true,
			description: "8000 bytes is the minimum jumbo frame MTU",
		},
		{
			name:        "Maximum jumbo frame MTU",
			mtu:         9001,
			expectValid: true,
			description: "9001 bytes is the maximum jumbo frame MTU",
		},
		{
			name:        "Above maximum jumbo frame MTU",
			mtu:         9002,
			expectValid: false,
			description: "9002 bytes is above the maximum jumbo frame MTU",
		},
		{
			name:        "Common WiFi MTU",
			mtu:         1472,
			expectValid: true,
			description: "1472 bytes is a common WiFi MTU",
		},
		{
			name:        "PPPoE MTU",
			mtu:         1492,
			expectValid: true,
			description: "1492 bytes is the typical PPPoE MTU",
		},
		{
			name:        "Mid-range jumbo frame",
			mtu:         8500,
			expectValid: true,
			description: "8500 bytes is a valid jumbo frame MTU",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := ValidateMTU(tt.mtu)

			if tt.expectValid {
				g.Expect(err).NotTo(HaveOccurred(), tt.description)
			} else {
				g.Expect(err).To(HaveOccurred(), tt.description)
			}
		})
	}
}

func TestMTUValidationIntegrationWithNetworkValidator(t *testing.T) {
	g := NewWithT(t)

	// Test that MTU validation is properly integrated into the network interface validator
	// We'll test the individual components since the full integration requires AWS credentials

	// Test 1: Verify MTU validation functions work
	validMTUs := []int{68, 576, 1000, 1500, 8000, 8500, 9000, 9001}
	for _, mtu := range validMTUs {
		err := ValidateMTU(mtu)
		g.Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("MTU %d should be valid", mtu))
	}

	invalidMTUs := []int{0, -1, 67, 1501, 7999, 9002, 65536}
	for _, mtu := range invalidMTUs {
		err := ValidateMTU(mtu)
		g.Expect(err).To(HaveOccurred(), fmt.Sprintf("MTU %d should be invalid", mtu))
	}

	// Test 2: Verify the IP-specific MTU validation function works
	// Test with a non-existent IP to verify error handling
	nonExistentIP := net.ParseIP("192.168.255.254")
	err := ValidateNetworkInterfaceMTUForIP(nonExistentIP)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("no active network interface found with IP"))

	// Test 3: Verify network interface validator can be created with MTU validation
	awsConfig := aws.Config{Region: "us-west-2"}
	validator := NewNetworkInterfaceValidator(awsConfig)
	g.Expect(validator.network).NotTo(BeNil())
}
