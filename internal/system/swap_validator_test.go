package system

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/aws/eks-hybrid/internal/api"
)

// mockInformer implements validation.Informer for testing
type mockInformer struct {
	startingCalled bool
	doneCalled     bool
	lastError      error
}

func (m *mockInformer) Starting(ctx context.Context, name, message string) {
	m.startingCalled = true
}

func (m *mockInformer) Done(ctx context.Context, name string, err error) {
	m.doneCalled = true
	m.lastError = err
}

func TestSwapValidator_Run(t *testing.T) {
	tests := []struct {
		name          string
		nodeConfig    *api.NodeConfig
		setupMockSwap func() // Function to set up mock swap state
		expectError   bool
		errorContains string
	}{
		{
			name:          "no swap present",
			nodeConfig:    &api.NodeConfig{},
			setupMockSwap: func() {}, // No setup needed for no swap
			expectError:   false,
		},
		{
			name: "failSwapOn set to false - swap allowed",
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Kubelet: api.KubeletOptions{
						Config: api.InlineDocument{
							"failSwapOn": createBoolRawExtension(false),
						},
					},
				},
			},
			setupMockSwap: func() {}, // Even with swap present, should pass
			expectError:   false,
		},
		{
			name: "failSwapOn set to true - default behavior",
			nodeConfig: &api.NodeConfig{
				Spec: api.NodeConfigSpec{
					Kubelet: api.KubeletOptions{
						Config: api.InlineDocument{
							"failSwapOn": createBoolRawExtension(true),
						},
					},
				},
			},
			setupMockSwap: func() {}, // No swap present, should pass
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			logger := zap.NewNop()
			validator := NewSwapValidator(logger)
			informer := &mockInformer{}
			ctx := context.Background()

			if tt.setupMockSwap != nil {
				tt.setupMockSwap()
			}

			// Execute
			err := validator.Run(ctx, informer, tt.nodeConfig)

			// Verify
			assert.True(t, informer.startingCalled, "Starting should be called")
			assert.True(t, informer.doneCalled, "Done should be called")

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Error(t, informer.lastError)
			} else {
				assert.NoError(t, err)
				assert.NoError(t, informer.lastError)
			}
		})
	}
}

// Helper function to create a runtime.RawExtension from a boolean value
func createBoolRawExtension(value bool) runtime.RawExtension {
	raw, _ := json.Marshal(value)
	return runtime.RawExtension{Raw: raw}
}

func TestNewSwapValidator(t *testing.T) {
	logger := zap.NewNop()
	validator := NewSwapValidator(logger)

	assert.NotNil(t, validator)
	assert.Equal(t, logger, validator.logger)
}
