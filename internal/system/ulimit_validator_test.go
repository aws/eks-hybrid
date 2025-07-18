package system

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
)

func TestUlimitValidator_Run(t *testing.T) {
	logger := zap.NewNop()
	validator := NewUlimitValidator(logger)

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
	logger := zap.NewNop()
	validator := NewUlimitValidator(logger)

	tests := []struct {
		name           string
		limits         map[string]*ulimit
		expectedIssues int
	}{
		{
			name: "no issues",
			limits: map[string]*ulimit{
				"nofile": {soft: 65536, hard: 65536},
				"nproc":  {soft: 32768, hard: 32768},
				"core":   {soft: 0, hard: 0},
			},
			expectedIssues: 0,
		},
		{
			name: "low nofile limits",
			limits: map[string]*ulimit{
				"nofile": {soft: 1024, hard: 4096},
				"nproc":  {soft: 32768, hard: 32768},
				"core":   {soft: 0, hard: 0},
			},
			expectedIssues: 2, // both soft and hard nofile limits are low
		},
		{
			name: "missing limits",
			limits: map[string]*ulimit{
				"core": {soft: 0, hard: 0},
			},
			expectedIssues: 2, // missing nofile and nproc
		},
		{
			name: "core file size issue",
			limits: map[string]*ulimit{
				"nofile": {soft: 65536, hard: 65536},
				"nproc":  {soft: 32768, hard: 32768},
				"core":   {soft: 1024, hard: 1024}, // non-zero, non-unlimited
			},
			expectedIssues: 1, // core file size issue
		},
		{
			name: "unlimited limits",
			limits: map[string]*ulimit{
				"nofile": {soft: ^uint64(0), hard: ^uint64(0)},
				"nproc":  {soft: ^uint64(0), hard: ^uint64(0)},
				"core":   {soft: ^uint64(0), hard: ^uint64(0)},
			},
			expectedIssues: 0, // unlimited is acceptable
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := validator.checkCriticalLimits(tt.limits)
			if len(issues) != tt.expectedIssues {
				t.Errorf("expected %d issues but got %d: %v", tt.expectedIssues, len(issues), issues)
			}
		})
	}
}

func TestNewUlimitValidator(t *testing.T) {
	logger := zap.NewNop()
	validator := NewUlimitValidator(logger)

	if validator == nil {
		t.Error("expected validator to be created")
	}
	if validator.logger != logger {
		t.Error("expected logger to be set correctly")
	}
}
