package nodevalidation

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/node/hybrid"
)

const (
	APIWaitTimeout = 3 * time.Minute
)

// ExecuteActiveNodeValidator runs the active node validation
func ExecuteActiveNodeValidator(ctx context.Context, logger *zap.Logger) error {
	// Create Kubernetes client once and reuse for all validations
	client, err := hybrid.BuildKubeClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes hybrid node client: %w", err)
	}

	// Node Registration validation
	nodeName, err := waitForNodeRegistration(ctx, client, APIWaitTimeout, logger)
	if err != nil {
		return fmt.Errorf("node registration validation failed: %w", err)
	}

	// CNI Detection
	_, err = waitForCNIDetection(ctx, client, nodeName, logger)
	if err != nil {
		return fmt.Errorf("CNI detection validation failed: %w", err)
	}

	// Node Readiness
	err = waitForNodeReadiness(ctx, client, nodeName, APIWaitTimeout, logger)
	if err != nil {
		return fmt.Errorf("node readiness validation failed: %w", err)
	}

	return nil
}
