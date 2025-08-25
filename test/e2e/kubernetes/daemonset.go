package kubernetes

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	ik8s "github.com/aws/eks-hybrid/internal/kubernetes"
)

const (
	daemonSetWaitTimeout = 3 * time.Minute
)

// GetDaemonSet returns a daemonset by name in a specific namespace
// It will wait for the daemonset to exist up to 3 minutes
func GetDaemonSet(ctx context.Context, logger logr.Logger, k8s kubernetes.Interface, namespace, name string) (*appsv1.DaemonSet, error) {
	ds, err := ik8s.GetAndWait(ctx, daemonSetWaitTimeout, k8s.AppsV1().DaemonSets(namespace), name, func(ds *appsv1.DaemonSet) bool {
		// Return true to stop polling as soon as we get the daemonset
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("waiting daemonset %s in namespace %s: %w", name, namespace, err)
	}
	return ds, nil
}

func DaemonSetWaitForReady(ctx context.Context, logger logr.Logger, k8s kubernetes.Interface, namespace, name string) error {
	if _, err := ik8s.GetAndWait(ctx, daemonSetWaitTimeout, k8s.AppsV1().DaemonSets(namespace), name, func(ds *appsv1.DaemonSet) bool {
		return ds.Status.NumberReady == ds.Status.DesiredNumberScheduled
	}); err != nil {
		return fmt.Errorf("daemonset %s replicas never became ready: %v", name, err)
	}
	return nil
}

// RestartDaemonSetAndWait restarts a DaemonSet and waits for rollout completion using Kubernetes API
func RestartDaemonSetAndWait(ctx context.Context, logger logr.Logger, k8s kubernetes.Interface, namespace, name string) error {
	logger.Info("Restarting DaemonSet ", "name", name, "namespace", namespace)

	// Patch DaemonSet to trigger restart
	now := time.Now().Format(time.RFC3339)
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}}}`, now)

	_, err := k8s.AppsV1().DaemonSets(namespace).Patch(ctx, name, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to restart DaemonSet %s: %w", name, err)
	}

	logger.Info("DaemonSet restart initiated - waiting for rollout to complete", "name", name)

	if err := DaemonSetWaitForReady(ctx, logger, k8s, namespace, name); err != nil {
		return fmt.Errorf("waiting for DaemonSet rollout: %w", err)
	}

	logger.Info("DaemonSet rollout completed successfully", "name", name, "namespace", namespace)
	return nil
}
