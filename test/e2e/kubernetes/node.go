package kubernetes

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	kRetry "k8s.io/client-go/util/retry"
	"k8s.io/kubectl/pkg/drain"

	ik8s "github.com/aws/eks-hybrid/internal/kubernetes"
	"github.com/aws/eks-hybrid/internal/retry"
	"github.com/aws/eks-hybrid/test/e2e/constants"
)

const (
	hybridNodeWaitTimeout    = 10 * time.Minute
	hybridNodeUpgradeTimeout = 2 * time.Minute
	nodeCordonTimeout        = 30 * time.Second
)

// WaitForNode wait for the node to join the cluster and fetches the node info which has the nodeName label
func WaitForNode(ctx context.Context, k8s kubernetes.Interface, nodeName string, logger logr.Logger) (*corev1.Node, error) {
	labelSelector := constants.TestInstanceNameKubernetesLabel + "=" + nodeName
	nodes, err := ik8s.ListAndWait(ctx, hybridNodeWaitTimeout, k8s.CoreV1().Nodes(), func(nodes *corev1.NodeList) bool {
		if len(nodes.Items) == 0 {
			logger.Info("Node with e2e label doesn't exist yet", "nodeName", nodeName)
			return false
		}
		logger.Info("Node with e2e label exists", "nodeName", nodeName)
		return true
	}, func(opts *ik8s.ListOptions) {
		opts.LabelSelector = labelSelector
	})

	if err == nil {
		if len(nodes.Items) > 1 {
			return nil, fmt.Errorf("found multiple nodes with e2e label %s: %v", nodeName, nodes.Items)
		}
		return &nodes.Items[0], nil
	}

	// Node not found by label - try direct node name lookup
	logger.Info("Node not found by e2e label, trying direct node name lookup ", "nodeName", nodeName)
	node, err := ik8s.GetAndWait(ctx, hybridNodeWaitTimeout, k8s.CoreV1().Nodes(), nodeName, func(node *corev1.Node) bool {
		if node.Status.Phase == corev1.NodeRunning || len(node.Status.Conditions) > 0 {
			logger.Info("Found node by direct name lookup", "nodeName", nodeName)
			return true
		}
		return false
	})
	if err != nil {
		return nil, fmt.Errorf("waiting for node %s to join the cluster : %w", nodeName, err)
	}
	return node, nil
}

func GetNodeInternalIP(node *corev1.Node) string {
	for _, address := range node.Status.Addresses {
		if address.Type == "InternalIP" {
			return address.Address
		}
	}
	return ""
}

func CheckForNodeWithE2ELabel(ctx context.Context, k8s kubernetes.Interface, nodeName string) (*corev1.Node, error) {
	return getNodeByE2ELabelName(ctx, k8s, nodeName)
}

// ListNodesWithLabels lists nodes that match the given label selector using robust retry
func ListNodesWithLabels(ctx context.Context, k8s kubernetes.Interface, labelSelector string) (*corev1.NodeList, error) {
	nodes, err := ik8s.ListRetry(ctx, k8s.CoreV1().Nodes(), func(opts *ik8s.ListOptions) {
		opts.LabelSelector = labelSelector
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes with selector %s: %w", labelSelector, err)
	}
	return nodes, nil
}

func getNodeByE2ELabelName(ctx context.Context, k8s kubernetes.Interface, nodeName string) (*corev1.Node, error) {
	nodes, err := ListNodesWithLabels(ctx, k8s, constants.TestInstanceNameKubernetesLabel+"="+nodeName)
	if err != nil {
		return nil, fmt.Errorf("listing nodes when looking for node with e2e label %s: %w", nodeName, err)
	}

	if len(nodes.Items) == 0 {
		return nil, nil
	}

	if len(nodes.Items) > 1 {
		return nil, fmt.Errorf("found multiple nodes with e2e label %s: %v", nodeName, nodes.Items)
	}

	return &nodes.Items[0], nil
}

// WaitForHybridNodeToBeReady will continue to poll until the node is:
// - marked ready
// - cilium-agent taint is removed (agent is ready on this node)
// - nodeNetworkAvailable is true
// - internal IP address is set
func WaitForHybridNodeToBeReady(ctx context.Context, k8s kubernetes.Interface, nodeName string, logger logr.Logger) (*corev1.Node, error) {
	node, err := ik8s.GetAndWait(ctx, hybridNodeWaitTimeout, k8s.CoreV1().Nodes(), nodeName, func(node *corev1.Node) bool {
		if !nodeReady(node) {
			logger.Info("Node is not ready yet", "node", nodeName)
		} else if !nodeCiliumAgentReady(node) {
			logger.Info("Node's cilium-agent is not ready yet. Verify the cilium-operator is running.", "node", nodeName)
		} else if !NodeNetworkAvailable(node) {
			logger.Info("Node is ready, but network is NetworkUnavailable condition not False", "node", nodeName)
		} else if GetNodeInternalIP(node) == "" {
			logger.Info("Node is ready, but internal IP address is not set", "node", nodeName)
		} else {
			logger.Info("Node is ready", "node", nodeName)
			return true
		}
		return false
	})
	if err != nil {
		return nil, fmt.Errorf("waiting for node %s to be ready: %w", nodeName, err)
	}
	return node, nil
}

func WaitForHybridNodeToBeNotReady(ctx context.Context, k8s kubernetes.Interface, nodeName string, logger logr.Logger) error {
	_, err := ik8s.GetAndWait(ctx, hybridNodeWaitTimeout, k8s.CoreV1().Nodes(), nodeName, func(node *corev1.Node) bool {
		if !nodeReady(node) {
			logger.Info("Node is not ready", "node", nodeName)
			return true
		}
		logger.Info("Node is still ready", "node", nodeName)
		return false
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
	err := ik8s.IdempotentDelete(ctx, k8s.CoreV1().Nodes(), name)
	if err != nil {
		return fmt.Errorf("deleting node: %w", err)
	}
	return nil
}

func EnsureNodeWithE2ELabelIsDeleted(ctx context.Context, k8s kubernetes.Interface, nodeName string) error {
	node, err := getNodeByE2ELabelName(ctx, k8s, nodeName)
	if err != nil {
		return err
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
	node, err := ik8s.GetAndWait(ctx, hybridNodeUpgradeTimeout, k8s.CoreV1().Nodes(), nodeName, func(node *corev1.Node) bool {
		kubernetesVersion := strings.TrimPrefix(node.Status.NodeInfo.KubeletVersion, "v")
		// If the current version matches the target version of kubelet, return true to stop polling
		if strings.HasPrefix(kubernetesVersion, targetVersion) {
			logger.Info("Node successfully upgraded to desired kubernetes version", "version", targetVersion)
			return true
		}
		return false
	})
	if err != nil {
		return nil, fmt.Errorf("waiting for node %s kubernetes version to be upgraded to %s: %w", nodeName, targetVersion, err)
	}
	return node, nil
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
	return kRetry.OnError(kRetry.DefaultBackoff, func(err error) bool {
		return true
	}, func() error {
		err := drain.RunNodeDrain(helper, node.Name)
		if err != nil {
			return fmt.Errorf("draining node %s: %v", node.Name, err)
		}
		return nil
	})
}

func UncordonNode(ctx context.Context, k8s kubernetes.Interface, node *corev1.Node) error {
	helper := &drain.Helper{
		Ctx:    ctx,
		Client: k8s,
	}
	return retry.NetworkRequest(ctx, func(ctx context.Context) error {
		err := drain.RunCordonOrUncordon(helper, node, false)
		if err != nil {
			return fmt.Errorf("cordoning node %s: %v", node.Name, err)
		}
		return nil
	})
}

func CordonNode(ctx context.Context, k8s kubernetes.Interface, node *corev1.Node, logger logr.Logger) error {
	helper := &drain.Helper{
		Ctx:    ctx,
		Client: k8s,
	}
	err := retry.NetworkRequest(ctx, func(ctx context.Context) error {
		err := drain.RunCordonOrUncordon(helper, node, true)
		if err != nil {
			return fmt.Errorf("cordoning node %s: %v", node.Name, err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Cordon returns before the node has been tainted and since we immediately run
	// drain, its possible (common) during our tests that pods get scheduled on the node after
	// drain gets the list of pods to evict and before the taint has been fully applied
	// leading to an error during nodeadm upgrade/uninstall due to non-daemonset pods running
	_, err = ik8s.GetAndWait(ctx, nodeCordonTimeout, k8s.CoreV1().Nodes(), node.Name, func(node *corev1.Node) bool {
		if nodeCordon(node) {
			logger.Info("Node successfully cordoned")
			return true
		}
		return false
	})
	if err != nil {
		return fmt.Errorf("waiting for node %s to be cordoned: %w", node.Name, err)
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

func NetworkUnavailableCondition(node *corev1.Node) *corev1.NodeCondition {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeNetworkUnavailable {
			return &condition
		}
	}
	return nil
}

// NodeNetworkAvailable returns true if the node has a network available condition with status false.
// If the condition is not present, although technically this mean it might have the a network available,
// it returns false.
// Most CNI will set this condition to true whenever they finish their setup. In particular,
// Cilium and Calico do it.
func NodeNetworkAvailable(node *corev1.Node) bool {
	condition := NetworkUnavailableCondition(node)
	return condition != nil && condition.Status == corev1.ConditionFalse
}

// FindNodeWithLabel finds a node that has the specified label key-value pair
func FindNodeWithLabel(ctx context.Context, k8s kubernetes.Interface, labelKey, labelValue string, logger logr.Logger) (string, error) {
	labelSelector := fmt.Sprintf("%s=%s", labelKey, labelValue)
	nodes, err := ListNodesWithLabels(ctx, k8s, labelSelector)
	if err != nil {
		return "", err
	}

	if len(nodes.Items) == 0 {
		return "", fmt.Errorf("no nodes found with label %s=%s", labelKey, labelValue)
	}

	nodeName := nodes.Items[0].Name
	logger.Info("Found node with label", "labelKey", labelKey, "labelValue", labelValue, "nodeName", nodeName)
	return nodeName, nil
}

// PatchNode patches a node with the given patch data
func PatchNode(ctx context.Context, k8s kubernetes.Interface, nodeName string, patchData []byte, logger logr.Logger) error {
	_, err := k8s.CoreV1().Nodes().Patch(ctx, nodeName, types.MergePatchType, patchData, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to patch node %s: %w", nodeName, err)
	}
	logger.Info("Successfully patched node", "node", nodeName)
	return nil
}

// LabelHybridNodesForTopology adds topology.kubernetes.io/zone=onprem label to hybrid nodes
func LabelHybridNodesForTopology(ctx context.Context, k8s kubernetes.Interface, logger logr.Logger) error {
	// Find all hybrid nodes
	nodes, err := ListNodesWithLabels(ctx, k8s, "eks.amazonaws.com/compute-type=hybrid")
	if err != nil {
		return fmt.Errorf("failed to list hybrid nodes: %w", err)
	}

	if len(nodes.Items) == 0 {
		logger.Info("No hybrid nodes found to label")
		return nil
	}

	// Label each hybrid node with topology zone
	for _, node := range nodes.Items {
		labelPatch := `{
			"metadata": {
				"labels": {
					"topology.kubernetes.io/zone": "onprem"
				}
			}
		}`

		err = PatchNode(ctx, k8s, node.Name, []byte(labelPatch), logger)
		if err != nil {
			return fmt.Errorf("failed to label hybrid node %s: %w", node.Name, err)
		}

		logger.Info("Labeled hybrid node with topology zone", "node", node.Name, "zone", "onprem")
	}

	return nil
}

// CountCoreDNSDistribution returns count of CoreDNS pods on hybrid vs cloud nodes
func CountCoreDNSDistribution(ctx context.Context, k8s kubernetes.Interface, pods *corev1.PodList, logger logr.Logger) (hybridCount, cloudCount int) {
	hybridCount = 0
	cloudCount = 0

	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning ||
			pod.Spec.NodeName == "" ||
			pod.DeletionTimestamp != nil {
			continue
		}

		node, err := k8s.CoreV1().Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{})
		if err != nil {
			logger.Info("Failed to get node details", "node", pod.Spec.NodeName, "error", err.Error())
			continue
		}

		if computeType, exists := node.Labels["eks.amazonaws.com/compute-type"]; exists && computeType == "hybrid" {
			hybridCount++
		} else {
			cloudCount++
		}
	}

	return hybridCount, cloudCount
}
