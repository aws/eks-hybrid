package retrier

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

// PollWithRetries polls until the context is done, an error is returned, or the condition returns true.
// It uses the standard validation parameters (interval, timeout) and implements the consecutive errors
// pattern used throughout the codebase.
func PollWithRetries(ctx context.Context, conditionFunc func(ctx context.Context) (bool, error)) error {
	consecutiveErrors := 0
	return wait.PollUntilContextTimeout(
		ctx,
		ValidationInterval,
		ValidationTimeout,
		true,
		func(ctx context.Context) (bool, error) {
			done, err := conditionFunc(ctx)
			if err != nil {
				consecutiveErrors++
				if consecutiveErrors >= ValidationMaxRetries {
					return false, err
				}
				return false, nil // continue polling
			}
			consecutiveErrors = 0 // Reset counter on success
			return done, nil
		},
	)
}

// PollWithRetriesCustom is similar to PollWithRetries but allows customizing the interval and timeout.
func PollWithRetriesCustom(ctx context.Context, interval, timeout time.Duration, maxRetries int, conditionFunc func(ctx context.Context) (bool, error)) error {
	consecutiveErrors := 0
	return wait.PollUntilContextTimeout(
		ctx,
		interval,
		timeout,
		true,
		func(ctx context.Context) (bool, error) {
			done, err := conditionFunc(ctx)
			if err != nil {
				consecutiveErrors++
				if consecutiveErrors >= maxRetries {
					return false, err
				}
				return false, nil // continue polling
			}
			consecutiveErrors = 0 // Reset counter on success
			return done, nil
		},
	)
}
