package kubernetes

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// WaitForDeploymentReady waits for a deployment to be ready
func WaitForDeploymentReady(ctx context.Context, logger logr.Logger, k8s kubernetes.Interface, namespace, name string) error {
	resourceName := fmt.Sprintf("Deployment %s", name)

	getResource := func(ctx context.Context) (any, error) {
		return k8s.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	}

	isResourceReady := func(resource any) bool {
		deployment := resource.(*appsv1.Deployment)
		return deployment.Status.ReadyReplicas == *deployment.Spec.Replicas
	}

	return GenericResourcePoller(ctx, logger, resourceName, getResource, isResourceReady)
}
