package kubernetes

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes"

	ik8s "github.com/aws/eks-hybrid/internal/kubernetes"
)

func DeploymentWaitForReplicas(ctx context.Context, timeout time.Duration, k8s kubernetes.Interface, namespace, name string) error {
	if _, err := ik8s.GetAndWait(ctx, timeout, k8s.AppsV1().Deployments(namespace), name, func(d *appsv1.Deployment) bool {
		return d.Status.Replicas == d.Status.ReadyReplicas
	}); err != nil {
		return fmt.Errorf("deployment %s replicas never became ready: %v", name, err)
	}

	return nil
}

// DeleteDeploymentAndWait deletes a deployment and waits for it to be fully removed
func DeleteDeploymentAndWait(ctx context.Context, k8s kubernetes.Interface, deploymentName, namespace string, logger logr.Logger) error {
	logger.Info("Deleting deployment and waiting for removal", "deployment", deploymentName)

	err := ik8s.IdempotentDelete(ctx, k8s.AppsV1().Deployments(namespace), deploymentName)
	if err != nil {
		return fmt.Errorf("deleting deployment %s in namespace %s: %w", deploymentName, namespace, err)
	}

	// Wait for deployment to be fully deleted
	_, err = ik8s.ListAndWait(ctx, 60*time.Second, k8s.AppsV1().Deployments(namespace), func(deployments *appsv1.DeploymentList) bool {
		for _, deployment := range deployments.Items {
			if deployment.Name == deploymentName {
				return false
			}
		}
		return true
	}, func(opts *ik8s.ListOptions) {
		opts.FieldSelector = "metadata.name=" + deploymentName
	})
	if err != nil {
		return fmt.Errorf("waiting for deployment %s to be deleted: %w", deploymentName, err)
	}

	logger.Info("Deployment fully deleted", "deployment", deploymentName)
	return nil
}
