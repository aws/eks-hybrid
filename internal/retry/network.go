package retry

import (
	"context"
	"time"
)

// NetworkRequest retries an operation with exponential backoff until it succeeds,
// max timeout is reached or context is cancelled. Default sensible values are used
// for total timeout, max consecutive errors and backoff.
// This is intended for "lightweight" network requests, like relatively fast HTTP API
// requests, and assumes a relatively fast network connection: response time sub 500ms.
// It's meant to be used for the majority of API requests we make in nodeadm, where this
// assumptions hold.
// If you are making heavy network requests, like downloading files or long polling for
// async operations, you should consider using a custom retrier.
func NetworkRequest(ctx context.Context, request func(context.Context) error, opts ...RetrierOption) error {
	// Default sensible values for network requests.
	r := Retrier{
		// Rule of thumb: if we don't succeed in 10s, this is not transient
		Timeout: 10 * time.Second,
		Backoff: Backoff{
			// Classic exponential backoff with 10% jitter
			// to avoid syncronized requests.
			Duration: 1 * time.Second,
			Factor:   2,
			Jitter:   0.1,
		},
	}

	for _, opt := range opts {
		opt(&r)
	}

	return r.Do(ctx, func(ctx context.Context) (bool, error) {
		if err := request(ctx); err != nil {
			return false, err
		}
		return true, nil
	})
}
