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

	"github.com/aws/eks-hybrid/internal/kubelet"
)

const defaultStaticPodManifestPath = "/etc/kubernetes/manifest"

const (
	nodeValidationInterval = 10 * time.Second
	nodeValidationTimeout  = 1 * time.Minute
)

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

	pods, err := getPodsOnNode(ctx, nodeName)
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

	clientset, err := kubelet.GetKubeClientFromKubeConfig()
	if err != nil {
		return nil, err
	}

	var node *v1.Node
	consecutiveErrors := 0
	err = wait.PollUntilContextTimeout(ctx, nodeValidationInterval, nodeValidationTimeout, true, func(ctx context.Context) (bool, error) {
		node, err = clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			consecutiveErrors += 1
			if consecutiveErrors > 5 {
				return false, errors.Wrap(err, "failed to get current node")
			}
			return false, nil // continue polling
		}
		return true, nil
	})

	return node, err
}
