package retrier

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPollWithRetries(t *testing.T) {
	t.Run("success on first try", func(t *testing.T) {
		callCount := 0
		err := PollWithRetries(context.Background(), func(ctx context.Context) (bool, error) {
			callCount++
			return true, nil
		})
		assert.NoError(t, err)
		assert.Equal(t, 1, callCount)
	})

	t.Run("success after retries", func(t *testing.T) {
		callCount := 0
		err := PollWithRetries(context.Background(), func(ctx context.Context) (bool, error) {
			callCount++
			if callCount < 3 {
				return false, errors.New("temporary error")
			}
			return true, nil
		})
		assert.NoError(t, err)
		assert.Equal(t, 3, callCount)
	})

	t.Run("failure after max retries", func(t *testing.T) {
		callCount := 0
		expectedErr := errors.New("persistent error")
		err := PollWithRetries(context.Background(), func(ctx context.Context) (bool, error) {
			callCount++
			return false, expectedErr
		})
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Equal(t, ValidationMaxRetries, callCount)
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		callCount := 0

		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		err := PollWithRetries(ctx, func(ctx context.Context) (bool, error) {
			callCount++
			time.Sleep(50 * time.Millisecond)
			return false, errors.New("temporary error")
		})

		assert.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled))
	})
}

func TestPollWithRetriesCustom(t *testing.T) {
	t.Run("custom parameters", func(t *testing.T) {
		callCount := 0
		customMaxRetries := 3
		expectedErr := errors.New("persistent error")

		err := PollWithRetriesCustom(
			context.Background(),
			5*time.Millisecond,
			50*time.Millisecond,
			customMaxRetries,
			func(ctx context.Context) (bool, error) {
				callCount++
				return false, expectedErr
			},
		)

		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Equal(t, customMaxRetries, callCount)
	})
}
