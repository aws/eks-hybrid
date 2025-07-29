package system

import (
	"context"
	"testing"

	"github.com/aws/eks-hybrid/internal/api"
)

func TestUlimitValidator_Run(t *testing.T) {
	validator := NewUlimitValidator()

	// Create a mock informer
	informer := &mockInformer{}

	// Create a mock node config
	nodeConfig := &api.NodeConfig{}

	ctx := context.Background()

	_ = validator.Run(ctx, informer, nodeConfig)
	// We expect this to work with real ulimit values or fail gracefully

	if !informer.startingCalled {
		t.Error("expected Starting to be called")
	}
	if !informer.doneCalled {
		t.Error("expected Done to be called")
	}
}

func TestCheckCriticalLimits(t *testing.T) {
	validator := NewUlimitValidator()

	tests := []struct {
		name           string
		noFileLimit    uint64
		nProcLimit     uint64
		expectedIssues int
	}{
		{
			name:           "no issues",
			noFileLimit:    65536,
			nProcLimit:     32768,
			expectedIssues: 0,
		},
		{
			name:           "low nofile limits",
			noFileLimit:    1000,
			nProcLimit:     32768,
			expectedIssues: 1,
		},
		{
			name:           "low nproc limits",
			noFileLimit:    65536,
			nProcLimit:     3000,
			expectedIssues: 1,
		},
		{
			name:           "unlimited limits",
			noFileLimit:    ^uint64(0),
			nProcLimit:     ^uint64(0),
			expectedIssues: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := validator.checkCriticalLimits(tt.noFileLimit, tt.nProcLimit)
			if len(issues) != tt.expectedIssues {
				t.Errorf("expected %d issues but got %d: %v", tt.expectedIssues, len(issues), issues)
			}
		})
	}
}
