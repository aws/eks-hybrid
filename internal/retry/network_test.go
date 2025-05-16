package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/retry"
)

func TestNetworkRequest(t *testing.T) {
	tests := []struct {
		name          string
		requestFunc   func(ctx context.Context) error
		ctxTimeout    time.Duration // 0 means no timeout on the context itself
		expectedError string
	}{
		{
			name: "succeeds on first try",
			requestFunc: func(ctx context.Context) error {
				return nil
			},
		},
		{
			name: "succeeds after a few retries",
			requestFunc: func() func(ctx context.Context) error {
				attempts := 0
				return func(ctx context.Context) error {
					attempts++
					if attempts < 3 {
						return errors.New("test error")
					}
					return nil
				}
			}(),
		},
		{
			name: "fails due to timeout",
			requestFunc: func(ctx context.Context) error {
				return errors.New("test error")
			},
			expectedError: "test error", // The last error should be this
		},
		{
			name: "fails due to context cancellation",
			requestFunc: func(ctx context.Context) error {
				// Simulate a long-running task that will be interrupted
				select {
				case <-time.After(15 * time.Second): // longer than NetworkRequest timeout
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
			ctxTimeout:    10 * time.Millisecond, // Cancel context quickly
			expectedError: "context deadline exceeded",
		},
		{
			name: "propagates last error on timeout",
			requestFunc: func() func(ctx context.Context) error {
				customErr := errors.New("custom persistent error")
				return func(ctx context.Context) error {
					return customErr
				}
			}(),
			expectedError: "custom persistent error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()
			if tt.ctxTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.ctxTimeout)
				defer cancel()
			}

			err := retry.NetworkRequest(ctx, tt.requestFunc,
				retry.WithTimeout(100*time.Millisecond),
				retry.WithBackoffDuration(1*time.Millisecond),
			)

			if tt.expectedError == "" {
				g.Expect(err).ToNot(HaveOccurred())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(tt.expectedError)))
			}
		})
	}
}
