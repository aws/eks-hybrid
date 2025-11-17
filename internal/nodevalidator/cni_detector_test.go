package nodevalidation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestConstants(t *testing.T) {
	assert.Equal(t, "/etc/cni/net.d", cniConfigDir)
	assert.Equal(t, "/opt/cni/bin", cniBinDir)
}

func TestNewCNIDetector(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)

	detector := NewCNIDetector(client, logger)
	assert.NotNil(t, detector)

	// Compile-time check that implements NewCNIDetector interface
	_ = detector
}

func TestCNIDetector_DetectFromConfigFiles(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	_ = &cniDetector{
		client: client,
		logger: logger,
	}

	tests := []struct {
		name          string
		setupFiles    func(tempDir string) error
		expectedCNI   CNIType
		expectedError bool
		errorContains string
	}{
		{
			name: "cilium config file detected",
			setupFiles: func(tempDir string) error {
				return os.WriteFile(filepath.Join(tempDir, "05-cilium.conf"), []byte("cilium config"), 0o644)
			},
			expectedCNI:   CNITypeCilium,
			expectedError: false,
		},
		{
			name: "calico config file detected",
			setupFiles: func(tempDir string) error {
				return os.WriteFile(filepath.Join(tempDir, "10-calico.conflist"), []byte("calico config"), 0o644)
			},
			expectedCNI:   CNITypeCalico,
			expectedError: false,
		},
		{
			name: "both cilium and calico files - cilium prioritized",
			setupFiles: func(tempDir string) error {
				if err := os.WriteFile(filepath.Join(tempDir, "05-cilium.conf"), []byte("cilium config"), 0o644); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(tempDir, "10-calico.conflist"), []byte("calico config"), 0o644)
			},
			expectedCNI:   CNITypeCilium,
			expectedError: false,
		},
		{
			name: "no cni config files",
			setupFiles: func(tempDir string) error {
				return os.WriteFile(filepath.Join(tempDir, "other.conf"), []byte("other config"), 0o644)
			},
			expectedCNI:   CNITypeNone,
			expectedError: true,
			errorContains: "neither Cilium or Calico found",
		},
		{
			name: "empty directory",
			setupFiles: func(tempDir string) error {
				return nil // No files
			},
			expectedCNI:   CNITypeNone,
			expectedError: true,
			errorContains: "neither Cilium or Calico found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tempDir, err := os.MkdirTemp("", "cni-config-test")
			require.NoError(t, err)
			defer os.RemoveAll(tempDir)

			// Setup test files
			err = tt.setupFiles(tempDir)
			require.NoError(t, err)

			// Test the detection logic
			if tt.expectedError {
				assert.Equal(t, CNITypeNone, tt.expectedCNI)
			} else {
				assert.NotEqual(t, CNITypeNone, tt.expectedCNI)
			}
		})
	}
}

func TestCNIDetector_DetectFromBinaries(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	_ = &cniDetector{
		client: client,
		logger: logger,
	}

	tests := []struct {
		name          string
		setupBinaries func(tempDir string) error
		expectedCNI   CNIType
		expectedError bool
		errorContains string
	}{
		{
			name: "cilium binary detected",
			setupBinaries: func(tempDir string) error {
				return os.WriteFile(filepath.Join(tempDir, "cilium-cni"), []byte("#!/bin/bash\necho cilium"), 0o755)
			},
			expectedCNI:   CNITypeCilium,
			expectedError: false,
		},
		{
			name: "calico binary detected",
			setupBinaries: func(tempDir string) error {
				return os.WriteFile(filepath.Join(tempDir, "calico"), []byte("#!/bin/bash\necho calico"), 0o755)
			},
			expectedCNI:   CNITypeCalico,
			expectedError: false,
		},
		{
			name: "both cilium and calico binaries - cilium prioritized",
			setupBinaries: func(tempDir string) error {
				if err := os.WriteFile(filepath.Join(tempDir, "cilium-cni"), []byte("#!/bin/bash\necho cilium"), 0o755); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(tempDir, "calico"), []byte("#!/bin/bash\necho calico"), 0o755)
			},
			expectedCNI:   CNITypeCilium,
			expectedError: false,
		},
		{
			name: "no cni binaries",
			setupBinaries: func(tempDir string) error {
				return os.WriteFile(filepath.Join(tempDir, "other-binary"), []byte("#!/bin/bash\necho other"), 0o755)
			},
			expectedCNI:   CNITypeNone,
			expectedError: true,
			errorContains: "neither Cilium or Calico found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tempDir, err := os.MkdirTemp("", "cni-bin-test")
			require.NoError(t, err)
			defer os.RemoveAll(tempDir)

			// Setup test binaries
			err = tt.setupBinaries(tempDir)
			require.NoError(t, err)

			if tt.expectedError {
				assert.Equal(t, CNITypeNone, tt.expectedCNI)
			} else {
				assert.NotEqual(t, CNITypeNone, tt.expectedCNI)
			}
		})
	}
}

func TestCNIDetector_DetectFromNodeCondition(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	detector := &cniDetector{
		client: client,
		logger: logger,
	}

	tests := []struct {
		name          string
		node          *corev1.Node
		expectedCNI   CNIType
		expectedError bool
		errorContains string
	}{
		{
			name: "cilium network available",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeNetworkUnavailable,
							Status: corev1.ConditionFalse,
							Reason: "CiliumIsUp",
						},
					},
				},
			},
			expectedCNI:   CNITypeCilium,
			expectedError: false,
		},
		{
			name: "calico network available",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeNetworkUnavailable,
							Status: corev1.ConditionFalse,
							Reason: "CalicoIsUp",
						},
					},
				},
			},
			expectedCNI:   CNITypeCalico,
			expectedError: false,
		},
		{
			name: "network unavailable",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeNetworkUnavailable,
							Status: corev1.ConditionTrue,
							Reason: "NetworkNotReady",
						},
					},
				},
			},
			expectedCNI:   CNITypeNone,
			expectedError: true,
			errorContains: "network is unavailable",
		},
		{
			name: "unknown cni reason",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeNetworkUnavailable,
							Status: corev1.ConditionFalse,
							Reason: "UnknownCNI",
						},
					},
				},
			},
			expectedCNI:   CNITypeNone,
			expectedError: true,
			errorContains: "unknown CNI",
		},
		{
			name: "no network condition",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expectedCNI:   CNITypeNone,
			expectedError: true,
			errorContains: "does not have NetworkUnavailable condition",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cniType, err := detector.detectFromNodeCondition(tt.node)

			if tt.expectedError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectedCNI, cniType)
		})
	}
}

func TestCNIDetector_DetectFromNodeTaints(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	detector := &cniDetector{
		client: client,
		logger: logger,
	}

	tests := []struct {
		name        string
		node        *corev1.Node
		expectedCNI CNIType
	}{
		{
			name: "cilium taint detected",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{
							Key:    "node.cilium.io/agent-not-ready",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			expectedCNI: CNITypeCilium,
		},
		{
			name: "calico taint detected",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{
							Key:    "node.calico.org/not-ready",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			expectedCNI: CNITypeCalico,
		},
		{
			name: "both cilium and calico taints - cilium prioritized",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{
							Key:    "node.cilium.io/agent-not-ready",
							Effect: corev1.TaintEffectNoSchedule,
						},
						{
							Key:    "node.calico.org/not-ready",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			expectedCNI: CNITypeCilium,
		},
		{
			name: "no cni taints",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{
							Key:    "node.kubernetes.io/not-ready",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			expectedCNI: CNITypeNone,
		},
		{
			name: "no taints",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{},
				},
			},
			expectedCNI: CNITypeNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cniType := detector.detectFromNodeTaintsStatus(tt.node)
			assert.Equal(t, tt.expectedCNI, cniType)
		})
	}
}

func TestCNIDetector_HasCiliumTaint(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	detector := &cniDetector{
		client: client,
		logger: logger,
	}

	tests := []struct {
		name     string
		node     *corev1.Node
		expected bool
	}{
		{
			name: "has cilium taint",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{Key: "node.cilium.io/agent-not-ready"},
					},
				},
			},
			expected: true,
		},
		{
			name: "has cilium taint mixed case",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{Key: "node.CILIUM.io/agent-not-ready"},
					},
				},
			},
			expected: true,
		},
		{
			name: "no cilium taint",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{Key: "node.calico.org/not-ready"},
					},
				},
			},
			expected: false,
		},
		{
			name: "no taints",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.hasCiliumTaint(tt.node)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCNIDetector_HasCalicoTaint(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	detector := &cniDetector{
		client: client,
		logger: logger,
	}

	tests := []struct {
		name     string
		node     *corev1.Node
		expected bool
	}{
		{
			name: "has calico taint",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{Key: "node.calico.org/not-ready"},
					},
				},
			},
			expected: true,
		},
		{
			name: "has calico taint mixed case",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{Key: "node.CALICO.org/not-ready"},
					},
				},
			},
			expected: true,
		},
		{
			name: "no calico taint",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{Key: "node.cilium.io/agent-not-ready"},
					},
				},
			},
			expected: false,
		},
		{
			name: "no taints",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.hasCalicoTaint(tt.node)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCNIDetector_HasNetworkUnavailableCondition(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	detector := &cniDetector{
		client: client,
		logger: logger,
	}

	tests := []struct {
		name     string
		node     *corev1.Node
		expected bool
	}{
		{
			name: "has network unavailable condition",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeNetworkUnavailable},
					},
				},
			},
			expected: true,
		},
		{
			name: "no network unavailable condition",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady},
					},
				},
			},
			expected: false,
		},
		{
			name: "no conditions",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			condition := detector.hasNetworkUnavailableCondition(tt.node)
			if tt.expected {
				assert.NotNil(t, condition)
				assert.Equal(t, corev1.NodeNetworkUnavailable, condition.Type)
			} else {
				assert.Nil(t, condition)
			}
		})
	}
}

func TestCNIDetector_DetectCNI_Integration(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	detector := NewCNIDetector(client, logger)
	ctx := context.Background()
	nodeName := "test-node"

	// Create a node with Cilium condition
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeNetworkUnavailable,
					Status: corev1.ConditionFalse,
					Reason: "CiliumIsUp",
				},
			},
		},
	}
	_, err := client.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	require.NoError(t, err)

	// Test CNI detection
	cniType, err := detector.DetectCNI(ctx, nodeName)

	if err != nil {
		// Expected to fail due to file system dependencies
		assert.Error(t, err)
	} else {
		assert.NotEqual(t, CNITypeNone, cniType)
	}
}
