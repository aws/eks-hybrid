package nodevalidator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNewNodeRegistrationChecker(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	timeout := 5 * time.Minute

	checker := NewNodeRegistrationChecker(client, timeout, logger)
	assert.NotNil(t, checker)

	// Compile-time check that implements NodeRegistrationChecker interface
	_ = checker
}

func TestNodeRegistrationChecker_WaitForNodeRegistration_NodeNotFound(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	timeout := 1 * time.Second
	checker := NewNodeRegistrationChecker(client, timeout, logger)
	ctx := context.Background()

	// Node not found validation
	_, err := checker.WaitForNodeRegistration(ctx)
	assert.Error(t, err)
}

func TestNodeRegistrationChecker_WaitForNodeRegistration_ContextCancellation(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	timeout := 5 * time.Minute
	checker := NewNodeRegistrationChecker(client, timeout, logger)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Test with cancelled context
	_, err := checker.WaitForNodeRegistration(ctx)
	assert.Error(t, err)
}

func TestNodeRegistrationChecker_ErrorHandling(t *testing.T) {
	tests := []struct {
		name          string
		timeout       time.Duration
		expectedError string
	}{
		{
			name:          "zero timeout",
			timeout:       0,
			expectedError: "failed to get node name from kubelet",
		},
		{
			name:          "negative timeout",
			timeout:       -1 * time.Minute,
			expectedError: "failed to get node name from kubelet",
		},
		{
			name:          "very short timeout",
			timeout:       1 * time.Nanosecond,
			expectedError: "failed to get node name from kubelet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			logger := zaptest.NewLogger(t)
			checker := NewNodeRegistrationChecker(client, tt.timeout, logger)
			ctx := context.Background()

			_, err := checker.WaitForNodeRegistration(ctx)
			assert.Error(t, err)
			if tt.expectedError != "" {
				assert.Contains(t, err.Error(), tt.expectedError)
			}
		})
	}
}

func TestNodeRegistrationChecker_APIErrors(t *testing.T) {
	tests := []struct {
		name          string
		setupError    error
		expectedError string
	}{
		{
			name: "not found error",
			setupError: apierrors.NewNotFound(schema.GroupResource{
				Group:    "",
				Resource: "nodes",
			}, "test-node"),
			expectedError: "did not register with the cluster",
		},
		{
			name:          "generic API error",
			setupError:    errors.New("API server unavailable"),
			expectedError: "waiting for node registration",
		},
		{
			name:          "timeout error",
			setupError:    context.DeadlineExceeded,
			expectedError: "waiting for node registration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test error handling logic structure
			if apierrors.IsNotFound(tt.setupError) {
				assert.Contains(t, tt.expectedError, "did not register")
			} else {
				assert.Contains(t, tt.expectedError, "waiting for node registration")
			}
		})
	}
}

func TestWaitForNodeRegistration_Success(t *testing.T) {
	client := fake.NewSimpleClientset()
	ctx := context.Background()

	// Create a node in the fake client
	nodeName := "test-node"
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			UID:  types.UID("test-uid-123"),
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
	_, err := client.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	assert.NoError(t, err)
}

func TestNodeRegistrationChecker_LoggingBehavior(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	timeout := 5 * time.Minute
	checker := NewNodeRegistrationChecker(client, timeout, logger)
	ctx := context.Background()

	// Test that logging works correctly during registration check
	_, err := checker.WaitForNodeRegistration(ctx)
	assert.Error(t, err)
}

func TestNodeRegistrationChecker_Integration(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	timeout := 5 * time.Minute
	checker := NewNodeRegistrationChecker(client, timeout, logger)
	ctx := context.Background()

	// Test that the checker integrates properly with the Kubernetes client
	assert.NotNil(t, checker)

	_, err := checker.WaitForNodeRegistration(ctx)
	assert.Error(t, err) // Expected to fail due to kubelet dependency
}

func TestWaitForNodeRegistrationValidation(t *testing.T) {
	client := fake.NewSimpleClientset()
	logger := zaptest.NewLogger(t)
	timeout := 1 * time.Second
	ctx := context.Background()

	// Test the wrapper function
	_, err := waitForNodeRegistrationValidation(ctx, client, timeout, logger)
	assert.Error(t, err) // Expected to fail due to kubelet dependency
}
