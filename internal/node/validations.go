package node

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/eks-hybrid/internal/kubelet"
)

const defaultStaticPodManifestPath = "/etc/kubernetes/manifest"

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

func IsDrained(ctx context.Context) error {
	podsOnNode, err := getPodsOnNode()
	if err != nil {
		return errors.Wrap(err, "failed to get pods on node")
	}

	for _, filter := range getDrainedPodFilters() {
		podsOnNode, err = filter(podsOnNode)
		if err != nil {
			return errors.Wrap(err, "running filter on pods")
		}
	}
	if len(podsOnNode) != 0 {
		return fmt.Errorf("node not drained")
	}
	return nil
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

	return clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
}
