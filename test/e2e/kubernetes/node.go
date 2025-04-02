package kubernetes

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/drain"
)

const (
	hybridNodeWaitTimeout    = 10 * time.Minute
	hybridNodeDelayInterval  = 5 * time.Second
	hybridNodeUpgradeTimeout = 2 * time.Minute
	nodeCordonDelayInterval  = 1 * time.Second
	nodeCordonTimeout        = 30 * time.Second
)

// WaitForNode wait for the node to join the cluster and fetches the node info from an internal IP address of the node
func WaitForNode(ctx context.Context, k8s kubernetes.Interface, internalIP string, logger logr.Logger) (*corev1.Node, error) {
	foundNode := &corev1.Node{}
	consecutiveErrors := 0
	err := wait.PollUntilContextTimeout(ctx, hybridNodeDelayInterval, hybridNodeWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		node, err := getNodeByInternalIP(ctx, k8s, internalIP)
		if err != nil {
			consecutiveErrors += 1
			if consecutiveErrors > 3 {
				return false, err
			}
			logger.Info("Retryable error listing nodes when looking for node with IP. Continuing to poll", "internalIP", internalIP, "error", err)
			return false, nil // continue polling
		}
		consecutiveErrors = 0
		if node != nil {
			foundNode = node
			return true, nil // node found, stop polling
		}

		logger.Info("Node with internal IP doesn't exist yet", "internalIP", internalIP)
		return false, nil // continue polling
	})
	if err != nil {
		return nil, err
	}
	return foundNode, nil
}

func getNodeByInternalIP(ctx context.Context, k8s kubernetes.Interface, internalIP string) (*corev1.Node, error) {
	nodes, err := k8s.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing nodes when looking for node with IP %s: %w", internalIP, err)
	}
	return nodeByInternalIP(nodes, internalIP), nil
}

func nodeByInternalIP(nodes *corev1.NodeList, nodeIP string) *corev1.Node {
	for _, node := range nodes.Items {
		for _, address := range node.Status.Addresses {
			if address.Type == "InternalIP" && address.Address == nodeIP {
				return &node
			}
		}
	}
	return nil
}

func WaitForHybridNodeToBeReady(ctx context.Context, k8s kubernetes.Interface, nodeName string, logger logr.Logger) error {
	consecutiveErrors := 0
	err := wait.PollUntilContextTimeout(ctx, nodePodDelayInterval, hybridNodeWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		node, err := k8s.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			logger.Info("Node does not exist yet", "node", nodeName)
			return false, nil
		}
		if err != nil {
			consecutiveErrors += 1
			if consecutiveErrors > 3 {
				return false, fmt.Errorf("getting hybrid node %s: %w", nodeName, err)
			}
			logger.Info("Retryable error getting hybrid node. Continuing to poll", "name", nodeName, "error", err)
			return false, nil // continue polling
		}
		consecutiveErrors = 0

		if !nodeReady(node) {
			logger.Info("Node is not ready yet", "node", nodeName)
		} else if !nodeCiliumAgentReady(node) {
			logger.Info("Node's cilium-agent is not ready yet. Verify the cilium-operator is running.", "node", nodeName)
		} else if !nodeNetworkAvailable(node) {
			logger.Info("Node is ready, but network is NetworkUnavailable condition not False", "node", nodeName)
		} else {
			logger.Info("Node is ready", "node", nodeName)
			return true, nil // node is ready, stop polling
		}

		return false, nil // continue polling
	})
	if err != nil {
		return fmt.Errorf("waiting for node %s to be ready: %w", nodeName, err)
	}

	return nil
}

func WaitForHybridNodeToBeNotReady(ctx context.Context, k8s kubernetes.Interface, nodeName string, logger logr.Logger) error {
	consecutiveErrors := 0
	err := wait.PollUntilContextTimeout(ctx, nodePodDelayInterval, hybridNodeWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		node, err := k8s.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			consecutiveErrors += 1
			if consecutiveErrors > 3 {
				return false, fmt.Errorf("getting hybrid node %s: %w", nodeName, err)
			}
			logger.Info("Retryable error getting hybrid node. Continuing to poll", "name", nodeName, "error", err)
			return false, nil // continue polling
		}
		consecutiveErrors = 0

		if !nodeReady(node) {
			logger.Info("Node is not ready", "node", nodeName)
			return true, nil // node is not ready, stop polling
		} else {
			logger.Info("Node is still ready", "node", nodeName)
		}

		return false, nil // continue polling
	})
	if err != nil {
		return fmt.Errorf("waiting for node %s to be not ready: %w", nodeName, err)
	}

	return nil
}

func nodeReady(node *corev1.Node) bool {
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

func nodeCiliumAgentReady(node *corev1.Node) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Key == "node.cilium.io/agent-not-ready" {
			return false
		}
	}
	return true
}

func DeleteNode(ctx context.Context, k8s kubernetes.Interface, name string) error {
	err := k8s.CoreV1().Nodes().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("deleting node: %w", err)
	}
	return nil
}

func EnsureNodeWithIPIsDeleted(ctx context.Context, k8s kubernetes.Interface, internalIP string) error {
	node, err := getNodeByInternalIP(ctx, k8s, internalIP)
	if err != nil {
		return fmt.Errorf("getting node by internal IP: %w", err)
	}
	if node == nil {
		return nil
	}

	err = DeleteNode(ctx, k8s, node.Name)
	if err != nil {
		return fmt.Errorf("deleting node %s: %w", node.Name, err)
	}
	return nil
}

func WaitForNodeToHaveVersion(ctx context.Context, k8s kubernetes.Interface, nodeName, targetVersion string, logger logr.Logger) (*corev1.Node, error) {
	foundNode := &corev1.Node{}
	consecutiveErrors := 0
	err := wait.PollUntilContextTimeout(ctx, nodePodDelayInterval, hybridNodeUpgradeTimeout, true, func(ctx context.Context) (done bool, err error) {
		node, err := k8s.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			consecutiveErrors += 1
			logger.Info("consecutiveErrors", "consecutiveErrors", consecutiveErrors)
			if consecutiveErrors > 3 {
				return false, fmt.Errorf("getting hybrid node %s: %w", nodeName, err)
			}
			logger.Info("Retryable error getting hybrid node. Continuing to poll", "name", nodeName, "error", err)
			return false, nil // continue polling
		}
		consecutiveErrors = 0

		kubernetesVersion := strings.TrimPrefix(node.Status.NodeInfo.KubeletVersion, "v")
		// If the current version matches the target version of kubelet, return true to stop polling
		if strings.HasPrefix(kubernetesVersion, targetVersion) {
			foundNode = node
			logger.Info("Node successfully upgraded to desired kubernetes version", "version", targetVersion)
			return true, nil
		}

		return false, nil // continue polling
	})
	if err != nil {
		return nil, fmt.Errorf("waiting for node %s kubernetes version to be upgraded to %s: %w", nodeName, targetVersion, err)
	}

	return foundNode, nil
}

func DrainNode(ctx context.Context, k8s kubernetes.Interface, node *corev1.Node) error {
	helper := &drain.Helper{
		Ctx:                             ctx,
		Client:                          k8s,
		Force:                           true, // Force eviction
		GracePeriodSeconds:              -1,   // Use pod's default grace period
		IgnoreAllDaemonSets:             true, // Ignore DaemonSet-managed pods
		DisableEviction:                 true, // forces drain to use delete rather than evict
		DeleteEmptyDirData:              true,
		SkipWaitForDeleteTimeoutSeconds: 0,
		Out:                             os.Stdout,
		ErrOut:                          os.Stderr,
	}

	err := drain.RunNodeDrain(helper, node.Name)
	if err != nil {
		return fmt.Errorf("draining node %s: %v", node.Name, err)
	}

	return nil
}

func UncordonNode(ctx context.Context, k8s kubernetes.Interface, node *corev1.Node) error {
	helper := &drain.Helper{
		Ctx:    ctx,
		Client: k8s,
	}

	err := drain.RunCordonOrUncordon(helper, node, false)
	if err != nil {
		return fmt.Errorf("cordoning node %s: %v", node.Name, err)
	}

	return nil
}

func CordonNode(ctx context.Context, k8s kubernetes.Interface, node *corev1.Node, logger logr.Logger) error {
	helper := &drain.Helper{
		Ctx:    ctx,
		Client: k8s,
	}

	err := drain.RunCordonOrUncordon(helper, node, true)
	if err != nil {
		return fmt.Errorf("cordoning node %s: %v", node.Name, err)
	}

	// Cordon returns before the node has been tainted and since we immediately run
	// drain, its possible (common) during our tests that pods get scheduled on the node after
	// drain gets the list of pods to evict and before the taint has been fully applied
	// leading to an error during nodeadm upgrade/uninstall due to non-daemonset pods running
	nodeName := node.ObjectMeta.Name
	consecutiveErrors := 0
	err = wait.PollUntilContextTimeout(ctx, nodeCordonDelayInterval, nodeCordonTimeout, true, func(ctx context.Context) (done bool, err error) {
		node, err := k8s.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			consecutiveErrors += 1
			logger.Info("consecutiveErrors", "consecutiveErrors", consecutiveErrors)
			if consecutiveErrors > 3 {
				return false, fmt.Errorf("getting node %s: %w", nodeName, err)
			}
			logger.Info("Retryable error getting hybrid node. Continuing to poll", "name", nodeName, "error", err)
			return false, nil // continue polling
		}
		consecutiveErrors = 0

		if nodeCordon(node) {
			logger.Info("Node successfully cordoned")
			return true, nil
		}

		return false, nil // continue polling
	})
	if err != nil {
		return fmt.Errorf("waiting for node %s to be cordoned: %w", nodeName, err)
	}

	return nil
}

func nodeCordon(node *corev1.Node) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Key == "node.kubernetes.io/unschedulable" {
			return true
		}
	}
	return false
}

// nodeNetworkAvailable returns true if the node has a NetworkUnavailable condition with status False
// both cilium and calico will set this condition when the agents start up
// ex:
// message: Calico is running on this node
// reason: CalicoIsUp
// status: "False"
// type: NetworkUnavailable
func nodeNetworkAvailable(node *corev1.Node) bool {
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeNetworkUnavailable && cond.Status == corev1.ConditionFalse {
			return true
		}
	}
	return false
}
