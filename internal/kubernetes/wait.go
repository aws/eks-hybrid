package kubernetes

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/aws/eks-hybrid/internal/retry"
)

const (
	minWait = 200 * time.Millisecond
	maxWait = 5 * time.Second
)

type Read[O runtime.Object] func(context.Context) (O, error)

// WaitFor waits for an object/s to meet a condition.
// It will retry until the timeout is reached or the condition is met.
// To allow for longer wait times while avoiding to retry non-transient errors,
// we only retry up to 3 consecutive errors coming from the API server.
func WaitFor[O runtime.Object](ctx context.Context, timeout time.Duration, read Read[O], ready func(O) bool) (O, error) {
	// Rule of thumb dynamic wait time calculation, we try 10ish times for small enough timeouts
	// Don't allow for wait times that are too small to avoid throttling.
	// or too long to avoid waiting longer than necessary, in this case we may retry more than 10 times.
	wait := max(timeout/10, minWait)
	wait = min(wait, maxWait)
	var obj O
	retrier := retry.Retrier{
		HandleError: retry.NewMaxConsecutiveErrorHandler(3),
		Timeout:     timeout,
		Backoff: retry.Backoff{
			Duration: wait,
		},
	}
	err := retrier.Do(ctx, func(ctx context.Context) (bool, error) {
		var err error
		obj, err = read(ctx)
		if err != nil {
			return false, err
		}

		return ready(obj), nil
	})

	return obj, err
}

// GetAndWait waits for an object to meet a condition.
// It will retry until the timeout is reached or the condition is met.
// To allow for longer wait times while avoiding to retry non-transient errors,
// we only retry up to 3 consecutive errors coming from the API server.
func GetAndWait[O runtime.Object](ctx context.Context, timeout time.Duration, get Getter[O], name string, ready func(O) bool) (O, error) {
	return WaitFor(ctx, timeout, func(ctx context.Context) (O, error) {
		return get.Get(ctx, name, metav1.GetOptions{})
	}, ready)
}

// ListAndWait waits for a list of objects to meet a condition.
// It will retry until the timeout is reached or the condition is met.
// To allow for longer wait times while avoiding to retry non-transient errors,
// we only retry up to 3 consecutive errors coming from the API server.
func ListAndWait[O runtime.Object](ctx context.Context, timeout time.Duration, list Lister[O], ready func(O) bool, opts ...ListOption) (O, error) {
	listOpt := &ListOptions{}
	for _, opt := range opts {
		opt(listOpt)
	}
	return WaitFor(ctx, timeout, func(ctx context.Context) (O, error) {
		return list.List(ctx, listOpt.ListOptions)
	}, ready)
}
