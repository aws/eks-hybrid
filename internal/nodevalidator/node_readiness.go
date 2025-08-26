package nodevalidation

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	k8s "github.com/aws/eks-hybrid/internal/kubernetes"
)

// nodeReadinessChecker implements NodeReadinessChecker interface
type nodeReadinessChecker struct {
	client  kubernetes.Interface
	timeout time.Duration
	logger  *zap.Logger
}

// NewNodeReadinessChecker creates a new NodeReadinessChecker
func NewNodeReadinessChecker(client kubernetes.Interface, timeout time.Duration, logger *zap.Logger) NodeReadinessChecker {
	return &nodeReadinessChecker{
		client:  client,
		timeout: timeout,
		logger:  logger,
	}
}

// WaitForNodeReadiness waits for the node to become ready
func (nrc *nodeReadinessChecker) WaitForNodeReadiness(ctx context.Context, nodeName string) error {
	// Wait for the node to be ready
	_, err := k8s.GetAndWait(ctx, nrc.timeout, nrc.client.CoreV1().Nodes(), nodeName, func(node *corev1.Node) bool {
		return node != nil && nrc.isNodeReady(node)
	})
	if err != nil {
		return fmt.Errorf("node '%s' did not become ready within timeout %v: %w", nodeName, nrc.timeout, err)
	}

	return nil
}

// isNodeReady checks if a node meets all readiness criteria
func (nrc *nodeReadinessChecker) isNodeReady(node *corev1.Node) bool {
	// Check if node has internal IP
	if !nrc.hasInternalIP(node) {
		nrc.logger.Error("Node does not have internal IP address", zap.String("nodeName", node.Name))
		return false
	}

	// Check network availability
	if !nrc.isNetworkAvailable(node) {
		nrc.logger.Error("Node network is not available", zap.String("nodeName", node.Name))
		return false
	}

	// Check basic node ready condition
	if !nrc.hasReadyCondition(node) {
		nrc.logger.Error("Node does not have Ready condition", zap.String("nodeName", node.Name))
		return false
	}

	return true
}

// hasReadyCondition checks if the node has Ready condition set to True
func (nrc *nodeReadinessChecker) hasReadyCondition(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// hasInternalIP checks if the node has an internal IP address
func (nrc *nodeReadinessChecker) hasInternalIP(node *corev1.Node) bool {
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeInternalIP && address.Address != "" {
			return true
		}
	}
	return false
}

// isNetworkAvailable checks if the node network is available
func (nrc *nodeReadinessChecker) isNetworkAvailable(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeNetworkUnavailable {
			// Network is available if NetworkUnavailable condition is False
			return condition.Status == corev1.ConditionFalse
		}
	}
	// If NetworkUnavailable condition is not present, assume network is available
	return true
}

// waitForNodeReadiness waits for node readiness with retry
func waitForNodeReadiness(ctx context.Context, client kubernetes.Interface, nodeName string, timeout time.Duration, logger *zap.Logger) error {
	statusCh := make(chan struct{})
	errCh := make(chan error)
	consecutiveErrors := 0

	logger.Info("Starting node readiness validation...")
	go func() {
		defer close(statusCh)
		defer close(errCh)

		for {
			if ctx.Err() != nil {
				return
			}

			// Create readiness checker and execute
			checker := NewNodeReadinessChecker(client, timeout, logger)
			err := checker.WaitForNodeReadiness(ctx, nodeName)
			if err != nil {
				consecutiveErrors++
				if consecutiveErrors > 3 || ctx.Err() != nil {
					errCh <- fmt.Errorf("node readiness check failed after multiple attempts: %v", err)
					return
				}
				time.Sleep(2 * time.Second)
			} else {
				// Success - node readiness completed
				statusCh <- struct{}{}
				return
			}
		}
	}()

	select {
	case <-statusCh:
		logger.Info("Node readiness validation completed successfully")
		return nil
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return fmt.Errorf("node readiness validation timeout occurred: %w", ctx.Err())
	}
}
