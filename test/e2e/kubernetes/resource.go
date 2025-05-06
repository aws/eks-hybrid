package kubernetes

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	resourceWaitTimeout  = 3 * time.Minute
	resourcePollInterval = 5 * time.Second
	maxConsecutiveErrors = 3
)

// GenericResourcePoller is a generic function to poll for kubernetes resources
func GenericResourcePoller(
	ctx context.Context,
	logger logr.Logger,
	resourceName string,
	getResource func(ctx context.Context) (any, error),
	isResourceReady func(resource any) bool,
) error {
	consecutiveErrors := 0

	err := wait.PollUntilContextTimeout(ctx, resourcePollInterval, resourceWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		resource, err := getResource(ctx)
		if err != nil {
			consecutiveErrors++
			if consecutiveErrors > maxConsecutiveErrors {
				return false, fmt.Errorf("getting %s: %w", resourceName, err)
			}
			logger.Info(fmt.Sprintf("Retryable error getting %s. Continuing to poll", resourceName),
				"name", resourceName, "error", err)
			return false, nil // continue polling
		}

		consecutiveErrors = 0
		if resource != nil && isResourceReady(resource) {
			return true, nil
		}

		return false, nil // continue polling
	})
	if err != nil {
		return fmt.Errorf("waiting for %s to be ready: %w", resourceName, err)
	}

	return nil
}
