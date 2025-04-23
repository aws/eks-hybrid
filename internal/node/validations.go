package node

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/aws/eks-hybrid/internal/kubelet"
	"github.com/aws/eks-hybrid/internal/node/hybrid"
)

const defaultStaticPodManifestPath = "/etc/kubernetes/manifest"

const (
	nodeValidationInterval   = 10 * time.Second
	nodeValidationTimeout    = 1 * time.Minute
	nodeValidationMaxRetries = 5
)

// NodeValidationOptions are options to configure node validations.
type NodeValidationOptions struct {
	ValidationInterval time.Duration
	ValidationTimeout  time.Duration
	MaxRetries         int
}

// NodeValidationOption defines a function type for setting options.
type NodeValidationOption func(*NodeValidationOptions)

// DefaultNodeValidationOptions returns the default configuration options.
func DefaultNodeValidationOptions() NodeValidationOptions {
	return NodeValidationOptions{
		ValidationInterval: nodeValidationInterval,
		ValidationTimeout:  nodeValidationTimeout,
		MaxRetries:         nodeValidationMaxRetries,
	}
}

// WithValidationInterval sets the validation interval.
func WithValidationInterval(interval time.Duration) NodeValidationOption {
	return func(opts *NodeValidationOptions) {
		opts.ValidationInterval = interval
	}
}

// WithValidationTimeout sets the validation timeout.
func WithValidationTimeout(timeout time.Duration) NodeValidationOption {
	return func(opts *NodeValidationOptions) {
		opts.ValidationTimeout = timeout
	}
}

// WithMaxRetries sets the maximum number of retries.
func WithMaxRetries(maxRetries int) NodeValidationOption {
	return func(opts *NodeValidationOptions) {
		opts.MaxRetries = maxRetries
	}
}

func IsUnscheduled(ctx context.Context) error {
	node, err := getCurrentNode(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if !node.Spec.Unschedulable {
		return fmt.Errorf("node is schedulable")
	}
	return nil
}

func IsDrained(ctx context.Context) (bool, error) {
	nodeName, err := kubelet.GetNodeName()
	if err != nil {
		return false, errors.Wrap(err, "getting node name from kubelet")
	}

	clientset, err := hybrid.BuildKubeClient()
	if err != nil {
		return false, errors.Wrap(err, "failed to create kubernetes client")
	}

	pods, err := GetPodsOnNode(ctx, nodeName, clientset)
	if err != nil {
		return false, errors.Wrapf(err, "getting pods for node %s", nodeName)
	}

	return isDrained(pods)
}

func isDrained(pods []v1.Pod) (bool, error) {
	for _, filter := range getDrainedPodFilters() {
		var err error
		pods, err = filter(pods)
		if err != nil {
			return false, errors.Wrap(err, "running filter on pods")
		}
	}

	return len(pods) == 0, nil
}

func IsInitialized(ctx context.Context) error {
	_, err := getCurrentNode(ctx)
	if err != nil {
		return err
	}
	return nil
}

func getCurrentNode(ctx context.Context) (*v1.Node, error) {
	nodeName, err := kubelet.GetNodeName()
	if err != nil {
		return nil, err
	}

	clientset, err := hybrid.BuildKubeClient()
	if err != nil {
		return nil, err
	}

	return getNode(ctx, nodeName, clientset)
}

func getNode(ctx context.Context, nodeName string, clientset kubernetes.Interface, options ...NodeValidationOption) (*v1.Node, error) {
	opts := DefaultNodeValidationOptions()

	for _, option := range options {
		option(&opts)
	}

	var node *v1.Node
	var err error
	consecutiveErrors := 0
	err = wait.PollUntilContextTimeout(ctx, opts.ValidationInterval, opts.ValidationTimeout, true, func(ctx context.Context) (bool, error) {
		node, err = clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			consecutiveErrors += 1
			if consecutiveErrors == opts.MaxRetries {
				return false, errors.Wrap(err, "failed to get current node")
			}
			return false, nil // continue polling
		}
		return true, nil
	})

	return node, err
}
