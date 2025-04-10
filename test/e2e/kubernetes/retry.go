package kubernetes

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
)

// Getter retrieves an object of type O from the Kubernetes API.
// It matches the Get signature of client-go clients.
type Getter[O runtime.Object] interface {
	Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (O, error)
}

// RetryGet retries the get request until it succeeds or the retry limit is reached.
func RetryGet[O runtime.Object](ctx context.Context, getter Getter[O], name string) (O, error) {
	var obj O
	err := retry.OnError(retry.DefaultRetry, func(err error) bool {
		// Retry any error type
		return true
	}, func() error {
		var err error
		obj, err = getter.Get(ctx, name, metav1.GetOptions{})
		return err
	})

	return obj, err
}

// List retrieves a list of objects from the Kubernetes API.
// It matches the List signature of client-go clients.
type Lister[O runtime.Object] interface {
	List(context.Context, metav1.ListOptions) (O, error)
}

// RetryList retries the list request until it succeeds or the retry limit is reached.
func RetryList[O runtime.Object](ctx context.Context, lister Lister[O]) (O, error) {
	var obj O
	err := retry.OnError(retry.DefaultRetry, func(err error) bool {
		// Retry any error type
		return true
	}, func() error {
		var err error
		obj, err = lister.List(ctx, metav1.ListOptions{})
		return err
	})

	return obj, err
}
