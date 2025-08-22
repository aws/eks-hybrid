package nodevalidation

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	"github.com/aws/eks-hybrid/internal/kubelet"
	k8s "github.com/aws/eks-hybrid/internal/kubernetes"
)

// nodeRegistrationChecker implements NodeRegistrationChecker interface
type nodeRegistrationChecker struct {
	client  kubernetes.Interface
	timeout time.Duration
	logger  *zap.Logger
}

// NewNodeRegistrationChecker creates a new NodeRegistrationChecker
func NewNodeRegistrationChecker(client kubernetes.Interface, timeout time.Duration, logger *zap.Logger) NodeRegistrationChecker {
	return &nodeRegistrationChecker{
		client:  client,
		timeout: timeout,
		logger:  logger,
	}
}

// WaitForNodeRegistration waits for the node to register with the Kubernetes cluster
func (nrc *nodeRegistrationChecker) WaitForNodeRegistration(ctx context.Context) (string, error) {
	// Get the node name from kubelet configuration
	nodeName, err := kubelet.GetNodeName()
	if err != nil {
		return "", fmt.Errorf("failed to get node name from kubelet: %w", err)
	}

	// Wait for the node availability
	node, err := k8s.GetAndWait(ctx, nrc.timeout, nrc.client.CoreV1().Nodes(), nodeName, func(node *corev1.Node) bool {
		// Node exists if we can retrieve it without error
		return node != nil
	})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("node '%s' did not register with the cluster within timeout %v", nodeName, nrc.timeout)
		}
		return "", fmt.Errorf("waiting for node registration: %w", err)
	}

	nrc.logger.Info("Node registered with cluster",
		zap.String("nodeName", nodeName),
		zap.String("nodeUID", string(node.UID)))

	return nodeName, nil
}

// waitForNodeRegistration waits for node registration with retry
func waitForNodeRegistration(ctx context.Context, client kubernetes.Interface, timeout time.Duration, logger *zap.Logger) (string, error) {
	statusCh := make(chan string)
	errCh := make(chan error)
	consecutiveErrors := 0

	logger.Info("Starting node registration validation...")
	go func() {
		defer close(statusCh)
		defer close(errCh)

		for {
			if ctx.Err() != nil {
				return
			}

			// Create registration checker and execute
			checker := NewNodeRegistrationChecker(client, timeout, logger)
			nodeName, err := checker.WaitForNodeRegistration(ctx)
			if err != nil {
				consecutiveErrors++
				if consecutiveErrors > 2 || ctx.Err() != nil {
					errCh <- fmt.Errorf("failed all multiple attempts: %v", err)
					return
				}
				time.Sleep(2 * time.Second)
			} else {
				// Success - node registration completed
				statusCh <- nodeName
				return
			}
		}
	}()

	select {
	case nodeName := <-statusCh:
		logger.Info("Node registration validation completed successfully", zap.String("nodeName", nodeName))
		return nodeName, nil
	case err := <-errCh:
		return "", err
	case <-ctx.Done():
		return "", fmt.Errorf("node registration validation timeout occurred: %w", ctx.Err())
	}
}
