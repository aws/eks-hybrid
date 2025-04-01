package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/retry"
	"k8s.io/kubectl/pkg/drain"

	"github.com/aws/eks-hybrid/test/e2e/constants"
)

const (
	nodePodWaitTimeout       = 3 * time.Minute
	nodePodDelayInterval     = 5 * time.Second
	hybridNodeWaitTimeout    = 10 * time.Minute
	hybridNodeDelayInterval  = 5 * time.Second
	hybridNodeUpgradeTimeout = 2 * time.Minute
	nodeCordonDelayInterval  = 1 * time.Second
	nodeCordonTimeout        = 30 * time.Second
	daemonSetWaitTimeout     = 3 * time.Minute
	daemonSetDelayInternal   = 5 * time.Second
	MinimumVersion           = "1.25"
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

func GetNginxPodName(name string) string {
	return "nginx-" + name
}

func CreateNginxPodInNode(ctx context.Context, k8s kubernetes.Interface, nodeName, namespace, region string, logger logr.Logger) error {
	podName := GetNginxPodName(nodeName)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "nginx",
					Image: fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com/ecr-public/nginx/nginx:latest", constants.EcrAccounId, region),
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 80,
						},
					},
					StartupProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/",
								Port: intstr.FromInt32(80),
							},
						},
						InitialDelaySeconds: 1,
						PeriodSeconds:       1,
						FailureThreshold:    int32(nodePodWaitTimeout.Seconds()),
					},
				},
			},
			// schedule the pod on the specific node using nodeSelector
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": nodeName,
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	return CreatePod(ctx, k8s, pod, logger)
}

func CreatePod(ctx context.Context, k8s kubernetes.Interface, pod *corev1.Pod, logger logr.Logger) error {
	podName := pod.ObjectMeta.Name
	namespace := pod.ObjectMeta.Namespace

	_, err := k8s.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating the test pod: %w", err)
	}

	podListOptions := metav1.ListOptions{
		FieldSelector: "metadata.name=" + podName,
	}
	err = waitForPodToBeRunning(ctx, k8s, podListOptions, namespace, logger)
	if err != nil {
		return fmt.Errorf("waiting for test pod to be running: %w", err)
	}
	return nil
}

func waitForPodToBeRunning(ctx context.Context, k8s kubernetes.Interface, listOptions metav1.ListOptions, namespace string, logger logr.Logger) error {
	consecutiveErrors := 0
	return wait.PollUntilContextTimeout(ctx, nodePodDelayInterval, nodePodWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		pods, err := k8s.CoreV1().Pods(namespace).List(ctx, listOptions)
		if err != nil {
			consecutiveErrors += 1
			if consecutiveErrors > 3 {
				return false, fmt.Errorf("getting test pod: %w", err)
			}
			logger.Info("Retryable error getting pod. Continuing to poll", "selector", listOptions.FieldSelector, "error", err)
			return false, nil // continue polling
		}
		consecutiveErrors = 0

		if len(pods.Items) == 0 {
			return false, nil // continue polling
		}

		if len(pods.Items) > 1 {
			return false, fmt.Errorf("found multiple pods for selector %s: %v", listOptions.FieldSelector, pods.Items)
		}

		pod := pods.Items[0]

		if pod.Status.Phase == corev1.PodSucceeded {
			return false, fmt.Errorf("test pod exited before containers ready")
		}
		if pod.Status.Phase != corev1.PodRunning {
			return false, nil // continue polling
		}

		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.ContainersReady && cond.Status != corev1.ConditionTrue {
				return false, nil // continue polling
			}
		}

		return true, nil // pod is running, stop polling
	})
}

func waitForPodToBeDeleted(ctx context.Context, k8s kubernetes.Interface, name, namespace string) error {
	return wait.PollUntilContextTimeout(ctx, nodePodDelayInterval, nodePodWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		_, err = k8s.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})

		if apierrors.IsNotFound(err) {
			return true, nil
		} else if err != nil {
			return false, err
		}

		return false, nil
	})
}

func DeletePod(ctx context.Context, k8s kubernetes.Interface, name, namespace string) error {
	err := k8s.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("deleting pod: %w", err)
	}
	return waitForPodToBeDeleted(ctx, k8s, name, namespace)
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

func PreviousVersion(kubernetesVersion string) (string, error) {
	currentVersion, err := version.ParseSemantic(kubernetesVersion + ".0")
	if err != nil {
		return "", fmt.Errorf("parsing version: %v", err)
	}
	prevVersion := fmt.Sprintf("%d.%d", currentVersion.Major(), currentVersion.Minor()-1)
	return prevVersion, nil
}

func IsPreviousVersionSupported(kubernetesVersion string) (bool, error) {
	prevVersion, err := PreviousVersion(kubernetesVersion)
	if err != nil {
		return false, err
	}
	minVersion := version.MustParseSemantic(MinimumVersion + ".0")
	return version.MustParseSemantic(prevVersion + ".0").AtLeast(minVersion), nil
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

// Retries up to 5 times to avoid connection errors
func GetPodLogsWithRetries(ctx context.Context, k8s kubernetes.Interface, name, namespace string) (logs string, err error) {
	err = retry.OnError(retry.DefaultRetry, func(err error) bool {
		// Retry any error type
		return true
	}, func() error {
		var err error
		logs, err = getPodLogs(ctx, k8s, name, namespace)
		return err
	})

	return logs, err
}

func getPodLogs(ctx context.Context, k8s kubernetes.Interface, name, namespace string) (string, error) {
	req := k8s.CoreV1().Pods(namespace).GetLogs(name, &corev1.PodLogOptions{})
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("opening log stream: %w", err)
	}
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	if _, err = io.Copy(buf, podLogs); err != nil {
		return "", fmt.Errorf("getting logs from stream: %w", err)
	}

	return buf.String(), nil
}

// Retries up to 5 times to avoid connection errors
func ExecPodWithRetries(ctx context.Context, config *restclient.Config, k8s kubernetes.Interface, name, namespace string, cmd ...string) (stdout, stderr string, err error) {
	err = retry.OnError(retry.DefaultBackoff, func(err error) bool {
		// Retry any error type
		return true
	}, func() error {
		var err error
		stdout, stderr, err = execPod(ctx, config, k8s, name, namespace, cmd...)
		return err
	})

	return stdout, stderr, err
}

// execPod returns the stdout and stderr even if the command fails and the err is non-nil
func execPod(ctx context.Context, config *restclient.Config, k8s kubernetes.Interface, name, namespace string, cmd ...string) (stdout, stderr string, err error) {
	req := k8s.CoreV1().RESTClient().Post().Resource("pods").Name(name).Namespace(namespace).SubResource("exec")
	req.VersionedParams(
		&corev1.PodExecOptions{
			Command: cmd,
			Stdin:   false,
			Stdout:  true,
			Stderr:  true,
			TTY:     true,
		},
		scheme.ParameterCodec,
	)
	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", "", err
	}
	var stdoutBuf, stderrBuf bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdoutBuf,
		Stderr: &stderrBuf,
	})

	return stdoutBuf.String(), stderrBuf.String(), err
}

func WaitForDaemonSetPodToBeRunning(ctx context.Context, k8s kubernetes.Interface, namespace, daemonSetName, nodeName string, logger logr.Logger) error {
	listOptions := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", "app.kubernetes.io/name", daemonSetName),
		FieldSelector: fmt.Sprintf("%s=%s", "spec.nodeName", nodeName),
	}
	return waitForPodToBeRunning(ctx, k8s, listOptions, namespace, logger)
}

func GetDaemonSet(ctx context.Context, logger logr.Logger, k8s kubernetes.Interface, namespace, name string) (*appsv1.DaemonSet, error) {
	var foundDaemonSet *appsv1.DaemonSet
	consecutiveErrors := 0
	err := wait.PollUntilContextTimeout(ctx, daemonSetDelayInternal, daemonSetWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		daemonSet, err := k8s.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			consecutiveErrors += 1
			if consecutiveErrors > 3 {
				return false, fmt.Errorf("getting daemonSet %s: %w", name, err)
			}
			logger.Info("Retryable error getting DaemonSet. Continuing to poll", "name", name, "error", err)
			return false, nil // continue polling
		}

		consecutiveErrors = 0
		if daemonSet != nil {
			foundDaemonSet = daemonSet
			return true, nil
		}

		return false, nil // continue polling
	})
	if err != nil {
		return nil, fmt.Errorf("waiting for DaemonSet %s to be ready: %w", name, err)
	}

	return foundDaemonSet, nil
}

func NewServiceAccount(ctx context.Context, logger logr.Logger, k8s kubernetes.Interface, namespace, name string) error {
	err := retry.OnError(retry.DefaultRetry, func(err error) bool {
		// Retry any error type
		return true
	}, func() error {
		if _, err := k8s.CoreV1().ServiceAccounts(namespace).Get(ctx, name, metav1.GetOptions{}); err == nil {
			logger.Info("Service account already exists", "namespace", namespace, "name", name)
			return nil
		}

		serviceAccount := &corev1.ServiceAccount{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ServiceAccount",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
		}

		if _, err := k8s.CoreV1().ServiceAccounts(namespace).Create(ctx, serviceAccount, metav1.CreateOptions{}); err != nil {
			return err
		}

		return nil
	})
	return err
}
