package addon

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/go-logr/logr"
	clientgo "k8s.io/client-go/kubernetes"

	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

type CloudWatchAddon struct {
	Addon
}

const (
	cloudwatchAddonName        = "amazon-cloudwatch-observability"
	cloudwatchNamespace        = "amazon-cloudwatch"
	cloudwatchComponentTimeout = 10 * time.Minute
	cloudwatchCheckInterval    = 15 * time.Second
)

// NewCloudWatchAddon creates a new CloudWatch Observability addon instance
func NewCloudWatchAddon(cluster string) CloudWatchAddon {
	return CloudWatchAddon{
		Addon: Addon{
			Cluster: cluster,
			Name:    cloudwatchAddonName,
		},
	}
}

// WaitForComponents waits for CloudWatch components to be ready
func (cw CloudWatchAddon) WaitForComponents(ctx context.Context, k8sClient clientgo.Interface, logger logr.Logger) error {
	timeout := time.After(cloudwatchComponentTimeout)
	ticker := time.NewTicker(cloudwatchCheckInterval)
	defer ticker.Stop()

	logger.Info("Waiting for CloudWatch components to be ready")

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for CloudWatch components to be ready")
		case <-ticker.C:
			// Check if namespace exists
			err := kubernetes.CheckNamespaceExists(ctx, k8sClient, cloudwatchNamespace)
			if err != nil {
				logger.Info("Waiting for CloudWatch namespace to be created")
				continue
			}

			// Check for operator deployment
			operators, err := kubernetes.ListDeploymentsWithLabels(ctx, k8sClient, cloudwatchNamespace, "control-plane=controller-manager")
			if err != nil || len(operators.Items) == 0 {
				logger.Info("Waiting for CloudWatch operator deployment")
				continue
			}

			operator := operators.Items[0]
			if operator.Status.ReadyReplicas != operator.Status.Replicas || operator.Status.ReadyReplicas == 0 {
				logger.Info("Waiting for operator pods to be ready", "ready", operator.Status.ReadyReplicas, "desired", operator.Status.Replicas)
				continue
			}

			logger.Info("CloudWatch webhook components are ready", "operator-ready", operator.Status.ReadyReplicas)
			return nil
		}
	}
}

// VerifyWebhookFunctionality tests CloudWatch webhook functionality in mixed mode
func (cw CloudWatchAddon) VerifyWebhookFunctionality(
	ctx context.Context,
	eksClient *eks.Client,
	k8sClient clientgo.Interface,
	clusterRegion string,
	hybridNodeSelector map[string]string,
	labels map[string]string,
	logger logr.Logger,
) error {
	logger.Info("Testing CloudWatch Observability webhook functionality")

	// Install and wait for CloudWatch addon
	if err := cw.Create(ctx, eksClient, logger); err != nil {
		return fmt.Errorf("installing CloudWatch Observability addon: %w", err)
	}

	if err := cw.WaitUntilActive(ctx, eksClient, logger); err != nil {
		return fmt.Errorf("waiting for addon to become active: %w", err)
	}

	if err := cw.WaitForComponents(ctx, k8sClient, logger); err != nil {
		return fmt.Errorf("waiting for CloudWatch components: %w", err)
	}

	// Find a hybrid node to test on
	hybridNodeName := ""
	for labelKey, labelValue := range hybridNodeSelector {
		nodeName, err := kubernetes.FindNodeWithLabel(ctx, k8sClient, labelKey, labelValue, logger)
		if err != nil {
			return fmt.Errorf("finding hybrid node with selector %s=%s: %w", labelKey, labelValue, err)
		}
		hybridNodeName = nodeName
		break
	}

	if hybridNodeName == "" {
		return fmt.Errorf("no hybrid node found matching selector")
	}

	podName := "cloudwatch-webhook-test-hybrid"

	if err := kubernetes.CreateNginxPodInNode(ctx, k8sClient, hybridNodeName, defaultNamespace, clusterRegion, logger, podName, labels); err != nil {
		return fmt.Errorf("creating and running CloudWatch test pod on hybrid node: %w", err)
	}

	logger.Info("CloudWatch Observability Addon test successful - cross-VPC webhook communication confirmed")
	return nil
}

// Cleanup performs CloudWatch-specific cleanup operations
func (cw CloudWatchAddon) Cleanup(ctx context.Context, k8sClient clientgo.Interface, namespace string, labels map[string]string, logger logr.Logger) error {
	// Clean up CloudWatch test pods using label selector
	labelSelector := fmt.Sprintf("test-suite=%s,app=cloudwatch-webhook-test-hybrid", labels["test-suite"])

	err := kubernetes.DeletePodsWithLabels(ctx, k8sClient, namespace, labelSelector, logger)
	if err != nil {
		logger.Info("Failed to delete CloudWatch test pods", "error", err.Error())
	}

	return nil
}
