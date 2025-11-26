package nodevalidator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNewNodeReadinessChecker(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	timeout := 5 * time.Minute

	checker := NewNodeReadinessChecker(client, timeout, logger)
	assert.NotNil(t, checker)

	// Compile-time check that implements NodeReadinessChecker interface
	_ = checker
}

func TestNodeReadinessChecker_IsNodeReady(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	timeout := 5 * time.Minute
	checker := &nodeReadinessChecker{
		client:  client,
		timeout: timeout,
		logger:  logger,
	}

	tests := []struct {
		name          string
		node          *corev1.Node
		expectedReady bool
	}{
		{
			name: "node ready with all conditions met",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{
							Type:    corev1.NodeInternalIP,
							Address: "10.0.0.1",
						},
					},
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
						{
							Type:   corev1.NodeNetworkUnavailable,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			expectedReady: true,
		},
		{
			name: "node not ready - missing ready condition",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{
							Type:    corev1.NodeInternalIP,
							Address: "10.0.0.1",
						},
					},
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionFalse,
						},
						{
							Type:   corev1.NodeNetworkUnavailable,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			expectedReady: false,
		},
		{
			name: "node not ready - missing internal IP",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{
							Type:    corev1.NodeExternalIP,
							Address: "203.0.113.1",
						},
					},
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
						{
							Type:   corev1.NodeNetworkUnavailable,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			expectedReady: false,
		},
		{
			name: "node not ready - network unavailable",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{
							Type:    corev1.NodeInternalIP,
							Address: "10.0.0.1",
						},
					},
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
						{
							Type:   corev1.NodeNetworkUnavailable,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expectedReady: false,
		},
		{
			name: "node ready - network condition missing",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{
							Type:    corev1.NodeInternalIP,
							Address: "10.0.0.1",
						},
					},
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expectedReady: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isReady := checker.isNodeReady(tt.node)
			assert.Equal(t, tt.expectedReady, isReady)
		})
	}
}

func TestNodeReadinessChecker_HasReadyCondition(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	timeout := 5 * time.Minute
	checker := &nodeReadinessChecker{
		client:  client,
		timeout: timeout,
		logger:  logger,
	}

	tests := []struct {
		name     string
		node     *corev1.Node
		expected bool
	}{
		{
			name: "has ready condition true",
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
			expected: true,
		},
		{
			name: "has ready condition false",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "has ready condition unknown",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionUnknown,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "no ready condition",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeNetworkUnavailable,
							Status: corev1.ConditionFalse,
						},
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
			hasReady := checker.hasReadyCondition(tt.node)
			assert.Equal(t, tt.expected, hasReady)
		})
	}
}

func TestNodeReadinessChecker_HasInternalIP(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	timeout := 5 * time.Minute
	checker := &nodeReadinessChecker{
		client:  client,
		timeout: timeout,
		logger:  logger,
	}

	tests := []struct {
		name     string
		node     *corev1.Node
		expected bool
	}{
		{
			name: "has internal IP",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{
							Type:    corev1.NodeInternalIP,
							Address: "10.0.0.1",
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "has multiple addresses including internal IP",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{
							Type:    corev1.NodeExternalIP,
							Address: "203.0.113.1",
						},
						{
							Type:    corev1.NodeInternalIP,
							Address: "10.0.0.1",
						},
						{
							Type:    corev1.NodeHostName,
							Address: "test-node",
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "has internal IP with empty address",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{
							Type:    corev1.NodeInternalIP,
							Address: "",
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "no internal IP",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{
							Type:    corev1.NodeExternalIP,
							Address: "203.0.113.1",
						},
						{
							Type:    corev1.NodeHostName,
							Address: "test-node",
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "no addresses",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasIP := checker.hasInternalIP(tt.node)
			assert.Equal(t, tt.expected, hasIP)
		})
	}
}

func TestNodeReadinessChecker_IsNetworkAvailable(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	timeout := 5 * time.Minute
	checker := &nodeReadinessChecker{
		client:  client,
		timeout: timeout,
		logger:  logger,
	}

	tests := []struct {
		name     string
		node     *corev1.Node
		expected bool
	}{
		{
			name: "network available - condition false",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeNetworkUnavailable,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "network unavailable - condition true",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeNetworkUnavailable,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "network unavailable - condition unknown",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeNetworkUnavailable,
							Status: corev1.ConditionUnknown,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "no network condition - assumed available",
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
			expected: true,
		},
		{
			name: "no conditions - assumed available",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isAvailable := checker.isNetworkAvailable(tt.node)
			assert.Equal(t, tt.expected, isAvailable)
		})
	}
}

func TestNodeReadinessChecker_WaitForNodeReadiness_Success(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	timeout := 5 * time.Minute
	checker := NewNodeReadinessChecker(client, timeout, logger)
	ctx := context.Background()
	nodeName := "test-node"

	// Create a ready node in the fake client
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "10.0.0.1",
				},
			},
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.NodeNetworkUnavailable,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}
	_, err := client.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	require.NoError(t, err)

	// Test waiting for node readiness
	err = checker.WaitForNodeReadiness(ctx, nodeName)
	assert.NoError(t, err)
}

func TestNodeReadinessChecker_WaitForNodeReadiness_NodeNotFound(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	timeout := 1 * time.Second // Short timeout for test
	checker := NewNodeReadinessChecker(client, timeout, logger)
	ctx := context.Background()
	nodeName := "non-existent-node"

	// Test waiting for non-existent node
	err := checker.WaitForNodeReadiness(ctx, nodeName)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "did not become ready")
}

func TestNodeReadinessChecker_WaitForNodeReadiness_ContextCancellation(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	timeout := 5 * time.Minute
	checker := NewNodeReadinessChecker(client, timeout, logger)
	nodeName := "test-node"

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Test with cancelled context
	err := checker.WaitForNodeReadiness(ctx, nodeName)
	assert.Error(t, err)
}

func TestWaitForNodeReadiness_Wrapper(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	timeout := 1 * time.Second
	ctx := context.Background()
	nodeName := "test-node"

	// Test the wrapper function
	err := waitForNodeReadiness(ctx, client, nodeName, timeout, logger)
	assert.Error(t, err) // Expected to fail as node doesn't exist
}

func TestNodeReadinessChecker_ErrorHandling(t *testing.T) {
	tests := []struct {
		name          string
		nodeName      string
		timeout       time.Duration
		expectedError string
	}{
		{
			name:          "empty node name",
			nodeName:      "",
			timeout:       5 * time.Minute,
			expectedError: "did not become ready",
		},
		{
			name:          "zero timeout",
			nodeName:      "test-node",
			timeout:       0,
			expectedError: "did not become ready",
		},
		{
			name:          "negative timeout",
			nodeName:      "test-node",
			timeout:       -1 * time.Minute,
			expectedError: "did not become ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			logger := zaptest.NewLogger(t)
			checker := NewNodeReadinessChecker(client, tt.timeout, logger)
			ctx := context.Background()

			err := checker.WaitForNodeReadiness(ctx, tt.nodeName)
			assert.Error(t, err)
			if tt.expectedError != "" {
				assert.Contains(t, err.Error(), tt.expectedError)
			}
		})
	}
}
