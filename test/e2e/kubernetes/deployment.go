package kubernetes

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	deploymentSetWaitTimeout   = 3 * time.Minute
	deploymentSetDelayInternal = 5 * time.Second
)

func WaitForDeploymentReady(ctx context.Context, logger logr.Logger, k8s kubernetes.Interface, namespace, name string) error {
	consecutiveErrors := 0
	err := wait.PollUntilContextTimeout(ctx, deploymentSetDelayInternal, deploymentSetWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		deployment, err := k8s.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			consecutiveErrors += 1
			if consecutiveErrors > 3 {
				return false, fmt.Errorf("getting deployment %s: %w", name, err)
			}
			logger.Info("Retryable error getting Deployment. Continuing to poll", "name", name, "error", err)
			return false, nil // continue polling
		}

		consecutiveErrors = 0
		if deployment != nil && deployment.Status.ReadyReplicas == *deployment.Spec.Replicas {
			return true, nil
		}

		return false, nil // continue polling
	})
	if err != nil {
		return fmt.Errorf("waiting for Deployment %s to be ready: %w", name, err)
	}

	return nil
}
