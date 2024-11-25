//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	nodePodWaitTimeout      = 3 * time.Minute
	nodePodDelayInterval    = 5 * time.Second
	hybridNodeWaitTimeout   = 10 * time.Minute
	hybridNodeDelayInterval = 5 * time.Second
	podNamespace            = "default"
)

// waitForNode wait for the node to join the cluster and fetches the node info from an internal IP address of the node
func waitForNode(ctx context.Context, k8s *kubernetes.Clientset, internalIP string, logger logr.Logger) (*corev1.Node, error) {
	foundNode := &corev1.Node{}
	consecutiveErrors := 0
	err := wait.PollUntilContextTimeout(ctx, hybridNodeDelayInterval, hybridNodeWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		nodes, err := k8s.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			consecutiveErrors += 1
			if consecutiveErrors > 3 {
				return false, fmt.Errorf("listing nodes when looking for node with IP %s: %w", internalIP, err)
			}
			logger.Info("Retryable error listing nodes when looking for node with IP. Continuing to poll", "internalIP", internalIP, "error", err)
			return false, nil // continue polling
		}
		consecutiveErrors = 0
		node := nodeByInternalIP(nodes, internalIP)
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

func waitForHybridNodeToBeReady(ctx context.Context, k8s *kubernetes.Clientset, nodeName string, logger logr.Logger) error {
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

		if nodeReady(node) {
			logger.Info("Node is ready", "node", nodeName)
			return true, nil // node is ready, stop polling
		} else {
			logger.Info("Node is not ready yet", "node", nodeName)
		}

		return false, nil // continue polling
	})
	if err != nil {
		return fmt.Errorf("waiting for node %s to be ready: %w", nodeName, err)
	}

	return nil
}

func waitForHybridNodeToBeNotReady(ctx context.Context, k8s *kubernetes.Clientset, nodeName string, logger logr.Logger) error {
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

func getNginxPodName(name string) string {
	return "nginx-" + name
}

func createNginxPodInNode(ctx context.Context, k8s *kubernetes.Clientset, nodeName string, logger logr.Logger) error {
	podName := getNginxPodName(nodeName)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: podNamespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "nginx",
					Image: "public.ecr.aws/nginx/nginx:latest",
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 80,
						},
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

	_, err := k8s.CoreV1().Pods(podNamespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating the test pod: %w", err)
	}

	err = waitForPodToBeRunning(ctx, k8s, podName, podNamespace, nodeName, logger)
	if err != nil {
		return fmt.Errorf("waiting for test pod to be running: %w", err)
	}
	return nil
}

func waitForPodToBeRunning(ctx context.Context, k8s *kubernetes.Clientset, name, namespace, nodeName string, logger logr.Logger) error {
	consecutiveErrors := 0
	return wait.PollUntilContextTimeout(ctx, nodePodDelayInterval, nodePodWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		pod, err := k8s.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			consecutiveErrors += 1
			if consecutiveErrors > 3 {
				return false, fmt.Errorf("getting test pod: %w", err)
			}
			logger.Info("Retryable error getting test pod. Continuing to poll", "name", name, "error", err)
			return false, nil // continue polling
		}
		consecutiveErrors = 0

		if pod.Status.Phase == corev1.PodRunning {
			return true, nil // pod is running, stop polling
		}
		return false, nil // continue polling
	})
}

func waitForPodToBeDeleted(ctx context.Context, k8s *kubernetes.Clientset, name, namespace string) error {
	return wait.PollUntilContextTimeout(ctx, nodePodDelayInterval, nodePodWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		_, err = k8s.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})

		if errors.IsNotFound(err) {
			return true, nil
		} else if err != nil {
			return false, err
		}

		return false, nil
	})
}

func deletePod(ctx context.Context, k8s *kubernetes.Clientset, name, namespace string) error {
	err := k8s.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("deleting pod: %w", err)
	}
	return waitForPodToBeDeleted(ctx, k8s, name, namespace)
}

func deleteNode(ctx context.Context, k8s *kubernetes.Clientset, name string) error {
	err := k8s.CoreV1().Nodes().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("deleting node: %w", err)
	}
	return nil
}

func getPodLogs(ctx context.Context, k8s *kubernetes.Clientset, name, namespace string) (string, error) {
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

func execPod(ctx context.Context, config *restclient.Config, k8s *kubernetes.Clientset, name, namespace string, cmd []string) (string, string, error) {
	req := k8s.CoreV1().RESTClient().Post().Resource("pods").Name(name).Namespace(namespace).SubResource("exec")
	req.VersionedParams(
		&v1.PodExecOptions{
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
	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return "", "", err
	}

	return stdout.String(), stderr.String(), nil
}
