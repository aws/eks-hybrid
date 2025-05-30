package kubernetes

import (
	"context"
	"fmt"
	"time"

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
