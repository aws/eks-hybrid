package kubernetes

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/aws/eks-hybrid/internal/retry"
)

// Getter retrieves an object of type O from the Kubernetes API.
// It matches the Get signature of client-go clients.
type Getter[O runtime.Object] interface {
	Get(ctx context.Context, name string, options metav1.GetOptions) (O, error)
}

// GetOptions configures a Get request.
type GetOptions struct {
	metav1.GetOptions
}

// GetOption is an option for the Get request.
type GetOption func(*GetOptions)

// GetRetry retries the get request until it succeeds or the retry limit is reached.
func GetRetry[O runtime.Object](ctx context.Context, getter Getter[O], name string, opts ...GetOption) (O, error) {
	getOpt := &GetOptions{}
	for _, opt := range opts {
		opt(getOpt)
	}

	var obj O
	err := retry.NetworkRequest(ctx, func(ctx context.Context) error {
		var err error
		obj, err = getter.Get(ctx, name, getOpt.GetOptions)
		return err
	})

	return obj, err
}

// List retrieves a list of objects from the Kubernetes API.
// It matches the List signature of client-go clients.
type Lister[O runtime.Object] interface {
	List(context.Context, metav1.ListOptions) (O, error)
}

// ListOptions configures a List request.
type ListOptions struct {
	metav1.ListOptions
}

// ListOption is an option for the List request.
type ListOption func(*ListOptions)

// ListRetry retries the list request until it succeeds or the retry limit is reached.
func ListRetry[O runtime.Object](ctx context.Context, lister Lister[O], opts ...ListOption) (O, error) {
	listOpt := &ListOptions{}
	for _, opt := range opts {
		opt(listOpt)
	}

	var obj O
	err := retry.NetworkRequest(ctx, func(ctx context.Context) error {
		var err error
		obj, err = lister.List(ctx, listOpt.ListOptions)
		return err
	})

	return obj, err
}
