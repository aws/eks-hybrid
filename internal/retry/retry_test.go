package retry_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/retry"
)

var (
	errTest         = errors.New("test error")
	errNonRetryable = errors.New("non-retryable error")
)

type mockOperation struct {
	currentAttempt int
	errToReturn    error
	sequence       []struct {
		done bool
		err  error
	}
	succeedAt int // attempt number to return (true, nil)
}

func (m *mockOperation) run(ctx context.Context) (bool, error) {
	m.currentAttempt++

	if m.currentAttempt <= len(m.sequence) {
		seq := m.sequence[m.currentAttempt-1]
		return seq.done, seq.err
	}

	if m.succeedAt > 0 && m.currentAttempt == m.succeedAt {
		return true, nil
	}

	if m.errToReturn != nil { // persistent error
		return false, m.errToReturn
	}

	return true, nil // Default success if not configured otherwise
}

func TestRetrier_Do(t *testing.T) {
	tests := []struct {
		name        string
		retrier     retry.Retrier
		op          func(ctx context.Context) (bool, error)
		ctxTimeout  time.Duration // For external context timeout
		expectedErr string        // Substring to match in error
	}{
		{
			name: "succeeds on first try",
			retrier: retry.Retrier{
				Timeout: 100 * time.Millisecond,
				Backoff: retry.Backoff{Duration: 1 * time.Millisecond, Steps: 5, Factor: 1.0, Jitter: 0.0},
			},
			op:          (&mockOperation{succeedAt: 1}).run,
			expectedErr: "",
		},
		{
			name: "succeeds after a few retries",
			retrier: retry.Retrier{
				Timeout: 100 * time.Millisecond,
				Backoff: retry.Backoff{Duration: 1 * time.Millisecond, Steps: 5, Factor: 1.0, Jitter: 0.0},
			},
			op:          (&mockOperation{errToReturn: errTest, succeedAt: 3}).run,
			expectedErr: "",
		},
		{
			name: "fails due to retrier timeout",
			retrier: retry.Retrier{
				Timeout: 20 * time.Millisecond, // Short timeout
				Backoff: retry.Backoff{Duration: 5 * time.Millisecond, Steps: 10, Factor: 1.0, Jitter: 0.0},
			},
			op:          (&mockOperation{errToReturn: errTest}).run, // Always errors
			expectedErr: fmt.Sprintf("%s while retrying: %s", context.DeadlineExceeded, errTest),
		},
		{
			name: "fails due to external context expiration",
			retrier: retry.Retrier{
				Timeout: 200 * time.Millisecond, // Long enough retrier timeout
				Backoff: retry.Backoff{Duration: 5 * time.Millisecond, Steps: 20, Factor: 1.0, Jitter: 0.0},
			},
			op: func(ctx context.Context) (bool, error) { // Operation that respects context
				time.Sleep(1 * time.Millisecond) // Simulate work
				return false, errTest            // This error will be the 'lastErr'
			},
			ctxTimeout:  10 * time.Millisecond, // External context times out sooner
			expectedErr: fmt.Sprintf("%s while retrying: %s", context.DeadlineExceeded, errTest),
		},
		{
			name: "stops after Backoff.Steps reached",
			retrier: retry.Retrier{
				Timeout: 100 * time.Millisecond,
				Backoff: retry.Backoff{Duration: 1 * time.Millisecond, Steps: 2, Factor: 1.0, Jitter: 0.0},
			},
			op:          (&mockOperation{errToReturn: errTest}).run, // Always errors
			expectedErr: fmt.Sprintf("while retrying: %s", errTest),
		},
		{
			name: "Backoff.Steps = 0 means effectively infinite (limited by timeout)",
			retrier: retry.Retrier{
				Timeout: 20 * time.Millisecond, // Short timeout
				Backoff: retry.Backoff{Duration: 1 * time.Millisecond, Steps: 0, Factor: 1.0, Jitter: 0.0},
			},
			op:          (&mockOperation{errToReturn: errTest}).run, // Always errors
			expectedErr: fmt.Sprintf("%s while retrying: %s", context.DeadlineExceeded, errTest),
		},
		{
			name: "HandleError returns non-retryable error",
			retrier: retry.Retrier{
				Timeout: 100 * time.Millisecond,
				Backoff: retry.Backoff{Duration: 1 * time.Millisecond, Steps: 5, Factor: 1.0, Jitter: 0.0},
				HandleError: func(err error) error {
					if errors.Is(err, errTest) {
						return errNonRetryable
					}
					return nil
				},
			},
			op:          (&mockOperation{errToReturn: errTest, succeedAt: 0}).run, // Returns errTest on first try
			expectedErr: errNonRetryable.Error(),
		},
		{
			name: "HandleError allows retry if it returns nil, leading to success",
			retrier: retry.Retrier{
				Timeout: 100 * time.Millisecond,
				Backoff: retry.Backoff{Duration: 1 * time.Millisecond, Steps: 5, Factor: 1.0, Jitter: 0.0},
				HandleError: func() func(error) error {
					count := 0
					return func(err error) error {
						if errors.Is(err, errTest) {
							count++
							if count == 1 { // Allow first error
								return nil
							}
							return errNonRetryable // Stop on subsequent
						}
						return nil
					}
				}(),
			},
			op:          (&mockOperation{errToReturn: errTest, succeedAt: 2}).run, // op will error once, then succeed
			expectedErr: "",
		},
		{
			name: "Operation returns done=true with an error",
			retrier: retry.Retrier{
				Timeout: 100 * time.Millisecond,
				Backoff: retry.Backoff{Duration: 1 * time.Millisecond, Steps: 5, Factor: 1.0, Jitter: 0.0},
			},
			op:          func(ctx context.Context) (bool, error) { return true, errTest },
			expectedErr: errTest.Error(),
		},
		{
			name: "Operation returns done=false, nil error, then succeeds",
			retrier: retry.Retrier{
				Timeout: 100 * time.Millisecond,
				Backoff: retry.Backoff{Duration: 1 * time.Millisecond, Steps: 5, Factor: 1.0, Jitter: 0.0},
			},
			op: (&mockOperation{
				sequence: []struct {
					done bool
					err  error
				}{
					{done: false, err: nil},
					{done: true, err: nil},
				},
			}).run,
		},
		{
			name: "Operation never done, nil error, times out (retrier timeout)",
			retrier: retry.Retrier{
				Timeout: 20 * time.Millisecond,
				Backoff: retry.Backoff{Duration: 5 * time.Millisecond, Steps: 10, Factor: 1.0, Jitter: 0.0},
			},
			op:          func(ctx context.Context) (bool, error) { return false, nil },
			expectedErr: context.DeadlineExceeded.Error(), // lastErr is nil, so original context.DeadlineExceeded
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()
			var cancel context.CancelFunc

			if tt.ctxTimeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, tt.ctxTimeout)
				defer cancel()
			}

			err := tt.retrier.Do(ctx, tt.op)

			if tt.expectedErr == "" {
				g.Expect(err).ToNot(HaveOccurred())
			} else {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring(tt.expectedErr)))
			}
		})
	}
}

func TestRetrier_Do_ContextAlreadyCancelled(t *testing.T) {
	g := NewWithT(t)
	op := (&mockOperation{errToReturn: errTest}).run

	r := retry.Retrier{
		Timeout: 100 * time.Millisecond,
		Backoff: retry.Backoff{Duration: 1 * time.Millisecond, Steps: 5},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := r.Do(ctx, op)

	g.Expect(err).To(HaveOccurred())
	// wait.ExponentialBackoffWithContext will check ctx.Done() first.
	// It will return ctx.Err() (which is context.Canceled).
	// lastErr in Do will be nil as op is not called.
	// So Do returns context.Canceled directly.
	g.Expect(errors.Is(err, context.Canceled)).To(BeTrue())
	g.Expect(err.Error()).To(ContainSubstring(context.Canceled.Error()))
}

func TestRetrier_Do_HandleError_ErrorIsNil(t *testing.T) {
	g := NewWithT(t)
	var handleErrorCalledWith error
	var opCalledCount int

	r := retry.Retrier{
		Timeout: 100 * time.Millisecond,
		Backoff: retry.Backoff{Duration: 1 * time.Millisecond, Steps: 3},
		HandleError: func(e error) error {
			handleErrorCalledWith = e
			if opCalledCount == 1 { // On first call (where op returns nil error)
				return errNonRetryable // Stop retries
			}
			return nil
		},
	}

	op := func(ctx context.Context) (bool, error) {
		opCalledCount++
		if opCalledCount == 1 {
			return false, nil // First time, no error, not done
		}
		return true, nil // Subsequent time, done
	}

	err := r.Do(context.Background(), op)
	g.Expect(err).To(HaveOccurred())
	g.Expect(errors.Is(err, errNonRetryable)).To(BeTrue())
	g.Expect(opCalledCount).To(Equal(1))        // Should call op once
	g.Expect(handleErrorCalledWith).To(BeNil()) // HandleError called with nil
}
