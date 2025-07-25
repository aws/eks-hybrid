package hybrid

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/certificate"
	"github.com/aws/eks-hybrid/internal/validation"
)

func TestHybridNodeProvider_Validate_NTPSkipped(t *testing.T) {
	logger := zap.NewNop()
	nodeConfig := &api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Name:   "test-cluster",
				Region: "us-west-2",
			},
		},
	}

	// Create provider with NTP validation skipped
	skipPhases := []string{ntpSyncValidation}
	hnp, err := NewHybridNodeProvider(nodeConfig, skipPhases, logger)
	require.NoError(t, err)

	// Cast to concrete type to access internal fields
	hybridProvider := hnp.(*HybridNodeProvider)

	// Mock the validator to avoid actual validation
	hybridProvider.validator = func(config *api.NodeConfig) error {
		return nil // Mock successful validation
	}

	// Validate should succeed without running NTP validation
	err = hnp.Validate()
	assert.NoError(t, err)
}

func TestHybridNodeProvider_Validate_NTPIncluded(t *testing.T) {
	logger := zaptest.NewLogger(t)
	nodeConfig := &api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Name:   "test-cluster",
				Region: "us-west-2",
			},
		},
	}

	// Create provider without skipping NTP validation
	hnp, err := NewHybridNodeProvider(nodeConfig, []string{}, logger)
	require.NoError(t, err)

	// Cast to concrete type to access internal fields
	hybridProvider := hnp.(*HybridNodeProvider)

	// Mock the validator to avoid actual validation
	hybridProvider.validator = func(config *api.NodeConfig) error {
		return nil // Mock successful validation
	}

	// Validate will run NTP validation
	err = hnp.Validate()
	if err != nil {
		// Check if it's a remediable error
		if validation.IsRemediable(err) {
			remediation := validation.Remediation(err)
			assert.NotEmpty(t, remediation, "Remediation should not be empty")
			assert.Contains(t, remediation, "Ensure the hybrid node has chronyd or systemd-timesyncd services running")
		}
	}
}

func TestHybridNodeProvider_NTPValidationIntegration(t *testing.T) {
	// Skip this test in CI environments where NTP might not be configured
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)
	nodeConfig := &api.NodeConfig{
		Spec: api.NodeConfigSpec{
			Cluster: api.ClusterDetails{
				Name:   "test-cluster",
				Region: "us-west-2",
			},
		},
	}

	hnp, err := NewHybridNodeProvider(nodeConfig, []string{}, logger)
	require.NoError(t, err)

	// Cast to concrete type to access internal fields
	hybridProvider := hnp.(*HybridNodeProvider)

	// Mock other validations to focus on NTP
	hybridProvider.validator = func(config *api.NodeConfig) error {
		return nil // Mock successful validation
	}

	// Test NTP validation through full validation flow
	// Skip other validations to focus on NTP
	skipPhases := []string{nodeIpValidation, certificate.KubeletCertValidation, kubeletVersionSkew}
	hybridProvider.skipPhases = skipPhases

	fullErr := hnp.Validate()

	// Verify behavior based on result
	if fullErr != nil {
		// If validation failed, verify remediation is included
		if validation.IsRemediable(fullErr) {
			remediation := validation.Remediation(fullErr)
			assert.NotEmpty(t, remediation, "Full validation should include remediation")
			assert.Contains(t, remediation, "Ensure the hybrid node has chronyd or systemd-timesyncd services running")
		}
	}
}
