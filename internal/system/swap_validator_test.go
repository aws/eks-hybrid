package system

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

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
		setupMockSwap func() // Function to set up mock swap state
		expectError   bool
		errorContains string
	}{
		{
			name:          "no swap present",
			setupMockSwap: func() {}, // No setup needed for no swap
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			validator := NewSwapValidator()
			informer := &mockInformer{}
			nodeConfig := &api.NodeConfig{}
			ctx := context.Background()

			if tt.setupMockSwap != nil {
				tt.setupMockSwap()
			}

			// Execute
			err := validator.Run(ctx, informer, nodeConfig)

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

func TestNewSwapValidator(t *testing.T) {
	validator := NewSwapValidator()

	assert.NotNil(t, validator)
}
