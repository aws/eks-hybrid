package retry

import (
	"context"
	"fmt"
	"math"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

// Retrier configures retries for an operation.
type Retrier struct {
	// HandleError is called after each operation retry.
	// If it returns an error, the operation is not retried
	// and that error is returned. This is useful for special
	// error handling, for example, for non retryable errors.
	// If HandleError is nil, the operation is always retried.
	HandleError HandleError
	// Timeout is the maximum time for the complete retry loop.
	// If zero, the operation is retried indefinitely until either
	// the backoff reaches its limit (if configured to do so) or
	// the context is cancelled.
	Timeout time.Duration
	// Backoff is the backoff configuration for the retry loop.
	Backoff Backoff
}

type (
	Backoff     wait.Backoff
	HandleError func(error) error
	// Operation is a process that can be retried.
	// It returns a boolean indicating if the operation is done.
	// Errors might be retried.
	Operation func(context.Context) (done bool, err error)
)

// Do retries an operation until it succeeds, max timeout is reached or context is cancelled.
func (r *Retrier) Do(ctx context.Context, op Operation) error {
	if r.Timeout != 0 {
		var cancel func()
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	backoff := wait.Backoff(r.Backoff)
	if r.Backoff.Steps == 0 {
		// If the number of steps is zero, we set it to the maximum possible value
		// to effectively remove the limit.
		// This is simply sane behavior for zero values, we should not make the caller
		// configure the limit unless they want to limit the number of steps. And a zero
		// steps limit doesn't make sense.
		// Unfortunately, wait.ExponentialBackoffWithContext does expect this field to be set
		// so we need to handle the "non-limit" case here.
		backoff.Steps = math.MaxInt
	}

	var lastErr error
	err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		done, err := op(ctx)
		if err != nil {
			lastErr = err
		}
		if r.HandleError != nil {
			if err = r.HandleError(err); err != nil {
				return false, err
			}
		}

		// If for some reason the operation returns true and an error, and the HandleError
		// wanted to retry, we want to return false to retry the operation.
		return done && err == nil, nil
	})

	// If the retry loop exited for any other reason but a non-retryable error
	// (context cancelled, timeout, backoff limit reached) then we wrap the last
	// error to give a better indication of what happened to the caller.
	if wait.Interrupted(err) && lastErr != nil {
		return fmt.Errorf("%s while retrying: %w", err, lastErr)
	}

	return err
}

// RetrierOption is a function that modifies a Retrier.
// Generally only for tests.
type RetrierOption func(*Retrier)

// WithTimeout sets the timeout for the retrier.
func WithTimeout(timeout time.Duration) RetrierOption {
	return func(r *Retrier) {
		r.Timeout = timeout
	}
}

// WithBackoffDuration sets the backoff duration for the retrier.
func WithBackoffDuration(duration time.Duration) RetrierOption {
	return func(r *Retrier) {
		r.Backoff.Duration = duration
	}
}
