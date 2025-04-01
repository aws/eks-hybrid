package kubernetes

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	daemonSetWaitTimeout   = 3 * time.Minute
	daemonSetDelayInternal = 5 * time.Second
)

func GetDaemonSet(ctx context.Context, logger logr.Logger, k8s kubernetes.Interface, namespace, name string) (*appsv1.DaemonSet, error) {
	var foundDaemonSet *appsv1.DaemonSet
	consecutiveErrors := 0
	err := wait.PollUntilContextTimeout(ctx, daemonSetDelayInternal, daemonSetWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		daemonSet, err := k8s.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			consecutiveErrors += 1
			if consecutiveErrors > 3 {
				return false, fmt.Errorf("getting daemonSet %s: %w", name, err)
			}
			logger.Info("Retryable error getting DaemonSet. Continuing to poll", "name", name, "error", err)
			return false, nil // continue polling
		}

		consecutiveErrors = 0
		if daemonSet != nil {
			foundDaemonSet = daemonSet
			return true, nil
		}

		return false, nil // continue polling
	})
	if err != nil {
		return nil, fmt.Errorf("waiting for DaemonSet %s to be ready: %w", name, err)
	}

	return foundDaemonSet, nil
}
