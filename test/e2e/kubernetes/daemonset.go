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
