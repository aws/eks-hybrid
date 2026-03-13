package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/retry"
	"k8s.io/kubectl/pkg/scheme"

	ik8s "github.com/aws/eks-hybrid/internal/kubernetes"
)

const (
	nodePodWaitTimeout               = 3 * time.Minute
	MaxCoreDNSRedistributionAttempts = 5
)

func GetNginxPodName(name string) string {
	return "nginx-" + name
}

func CreateNginxPodInNode(ctx context.Context, k8s kubernetes.Interface, nodeName, namespace, region string, logger logr.Logger, dnsSuffix, ecrAccount string, options ...interface{}) error {
	var podName string
	var labels map[string]string

	// Parse optional parameters
	for _, option := range options {
		switch v := option.(type) {
		case string:
			if v != "" {
				podName = v
			}
		case map[string]string:
			labels = v
		}
	}

	// Use default pod name if not provided
	if podName == "" {
		podName = GetNginxPodName(nodeName)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "nginx",
					Image: fmt.Sprintf("%s.dkr.ecr.%s.%s/ecr-public/nginx/nginx:latest", ecrAccount, region, dnsSuffix),
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
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": nodeName,
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	return CreatePod(ctx, k8s, pod, logger)
}

func CreatePod(ctx context.Context, k8s kubernetes.Interface, pod *corev1.Pod, logger logr.Logger) error {
	podName := pod.Name
	namespace := pod.Namespace

	err := ik8s.IdempotentCreate(ctx, k8s.CoreV1().Pods(namespace), pod)
	if err != nil {
		return fmt.Errorf("creating pod %s/%s: %w", namespace, podName, err)
	}

	podListOptions := metav1.ListOptions{
		FieldSelector: "metadata.name=" + podName,
	}
	err = WaitForPodsToBeRunning(ctx, k8s, podListOptions, namespace, logger)
	if err != nil {
		return fmt.Errorf("waiting for test pod to be running: %w", err)
	}
	return nil
}

// WaitForPodsToBeRunning waits until a pod is in running phase and all containers are ready with default timeout.
func WaitForPodsToBeRunning(ctx context.Context, k8s kubernetes.Interface, listOptions metav1.ListOptions, namespace string, logger logr.Logger) error {
	return WaitForPodsToBeRunningWithTimeout(ctx, k8s, listOptions, namespace, logger, nodePodWaitTimeout)
}

// WaitForPodsToBeRunningWithTimeout waits until a pod is in running phase and all containers are ready.
// It will return an error if any of the pods have already exited.
func WaitForPodsToBeRunningWithTimeout(ctx context.Context, k8s kubernetes.Interface, listOptions metav1.ListOptions, namespace string, logger logr.Logger, timeout time.Duration) error {
	pods, err := ik8s.ListAndWait(ctx, timeout, k8s.CoreV1().Pods(namespace), func(pods *corev1.PodList) bool {
		if len(pods.Items) == 0 {
			// keep polling
			return false
		}

		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodSucceeded {
				// we will return an error if the pod has already exited
				continue
			}
			if pod.Status.Phase != corev1.PodRunning {
				return false
			}
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.ContainersReady && cond.Status != corev1.ConditionTrue {
					return false
				}
			}
		}

		// stop polling
		return true
	}, func(opts *ik8s.ListOptions) {
		opts.ListOptions = listOptions
	})
	if err != nil {
		return fmt.Errorf("waiting for pod to be running with selector %s: %w", listOptions.FieldSelector, err)
	}

	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded {
			return fmt.Errorf("pod %s/%s already exited", pod.Namespace, pod.Name)
		}
	}

	return nil
}

// WaitForPodToBeCompleted waits until the pod is in Completed phase.
func WaitForPodToBeCompleted(ctx context.Context, k8s kubernetes.Interface, name, namespace string) error {
	_, err := ik8s.GetAndWait(ctx, nodePodWaitTimeout, k8s.CoreV1().Pods(namespace), name, func(pod *corev1.Pod) bool {
		return pod != nil && pod.Status.Phase == corev1.PodSucceeded
	})
	if err != nil {
		return fmt.Errorf("waiting for pod %s in namespace %s to be completed: %w", name, namespace, err)
	}

	return nil
}

func waitForPodToBeDeleted(ctx context.Context, k8s kubernetes.Interface, name, namespace string) error {
	_, err := ik8s.ListAndWait(ctx, nodePodWaitTimeout, k8s.CoreV1().Pods(namespace), func(pods *corev1.PodList) bool {
		return len(pods.Items) == 0
	}, func(opts *ik8s.ListOptions) {
		opts.FieldSelector = "metadata.name=" + name
	})
	if err != nil {
		return fmt.Errorf("waiting for pod %s in namespace %s to be deleted: %w", name, namespace, err)
	}
	return nil
}

func DeletePod(ctx context.Context, k8s kubernetes.Interface, name, namespace string) error {
	err := ik8s.IdempotentDelete(ctx, k8s.CoreV1().Pods(namespace), name)
	if err != nil {
		return fmt.Errorf("deleting pod %s in namespace %s: %w", name, namespace, err)
	}
	return waitForPodToBeDeleted(ctx, k8s, name, namespace)
}

// FetchLogs retrieves logs for all pods with the specified selectors and returns it in a single string.
func FetchLogs(ctx context.Context, k8s kubernetes.Interface, name, namespace string, options ...ik8s.ListOption) (string, error) {
	var pods *corev1.PodList

	pods, err := ik8s.ListRetry(ctx, k8s.CoreV1().Pods(namespace), options...)
	if err != nil {
		return "", fmt.Errorf("failed to get pods for %s after retries: %v", name, err)
	}

	var combinedLogs string
	for _, pod := range pods.Items {
		logs, err := GetPodLogsWithRetries(ctx, k8s, pod.Name, pod.Namespace)
		if err != nil {
			return "", err
		}
		combinedLogs = fmt.Sprintf("%s\n Logs for %s\n: %s", combinedLogs, pod.Name, logs)
	}

	return combinedLogs, err
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

// TestPodToPodConnectivity tests direct pod-to-pod connectivity using HTTP
func TestPodToPodConnectivity(ctx context.Context, config *restclient.Config, k8s kubernetes.Interface, clientPodName, targetPodName, namespace string, logger logr.Logger) error {
	targetPod, err := k8s.CoreV1().Pods(namespace).Get(ctx, targetPodName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting target pod %s: %w", targetPodName, err)
	}

	if targetPod.Status.PodIP == "" {
		return fmt.Errorf("target pod %s should have IP", targetPodName)
	}

	targetIP := targetPod.Status.PodIP
	logger.Info("Testing pod-to-pod connectivity", "from", clientPodName, "to", targetPodName, "ip", targetIP)

	cmd := []string{"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", fmt.Sprintf("http://%s:80", targetIP), "--connect-timeout", "90", "--max-time", "120"}

	_, err = ik8s.GetAndWait(ctx, 2*time.Minute, k8s.CoreV1().Pods(namespace), clientPodName, func(pod *corev1.Pod) bool {
		if pod.Status.Phase != corev1.PodRunning {
			return false
		}

		result, _, execErr := execPod(ctx, config, k8s, clientPodName, namespace, cmd...)
		if execErr != nil {
			logger.Info("Pod connectivity test failed", "error", execErr.Error())
			return false
		}

		return strings.Contains(result, "200")
	})
	if err != nil {
		return fmt.Errorf("connectivity from %s to %s failed: %w", clientPodName, targetPodName, err)
	}

	logger.Info("Pod-to-pod connectivity successful", "from", clientPodName, "to", targetPodName)
	return nil
}

func WaitForDaemonSetPodToBeRunning(ctx context.Context, k8s kubernetes.Interface, namespace, daemonSetName, nodeName string, logger logr.Logger) error {
	listOptions := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", "app.kubernetes.io/name", daemonSetName),
		FieldSelector: fmt.Sprintf("%s=%s", "spec.nodeName", nodeName),
	}
	return WaitForPodsToBeRunning(ctx, k8s, listOptions, namespace, logger)
}

// ListPodsWithLabels lists pods in a namespace that match the given label selector
func ListPodsWithLabels(ctx context.Context, k8s kubernetes.Interface, namespace, labelSelector string) (*corev1.PodList, error) {
	pods, err := k8s.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods with selector %s: %w", labelSelector, err)
	}
	return pods, nil
}

// DeletePodsWithLabels deletes all pods in a namespace that match the given label selector
func DeletePodsWithLabels(ctx context.Context, k8s kubernetes.Interface, namespace, labelSelector string, logger logr.Logger) error {
	pods, err := ListPodsWithLabels(ctx, k8s, namespace, labelSelector)
	if err != nil {
		return fmt.Errorf("failed to list pods for deletion: %w", err)
	}

	for _, pod := range pods.Items {
		// Force delete pods to prevent resource accumulation issues
		gracePeriodSeconds := int64(0)
		deleteOptions := metav1.DeleteOptions{
			GracePeriodSeconds: &gracePeriodSeconds,
		}
		if err := k8s.CoreV1().Pods(namespace).Delete(ctx, pod.Name, deleteOptions); err != nil {
			logger.Info("Pod cleanup: resource not found or already deleted", "name", pod.Name)
		} else {
			logger.Info("Force deleted pod", "name", pod.Name)
		}
	}
	return nil
}

// VerifyCoreDNSDistribution waits for CoreDNS pods to be distributed across node types
func VerifyCoreDNSDistribution(ctx context.Context, k8s kubernetes.Interface, timeout time.Duration, maxDeletions int, logger logr.Logger) error {
	deletionCount := 0

	_, err := ik8s.ListAndWait(ctx, timeout, k8s.CoreV1().Pods("kube-system"),
		func(pods *corev1.PodList) bool {
			hybridNodes, cloudNodes := CountCoreDNSDistribution(ctx, k8s, pods, logger)

			if hybridNodes > 0 && cloudNodes > 0 {
				logger.Info("CoreDNS properly distributed across node types",
					"hybridPods", hybridNodes, "cloudPods", cloudNodes)
				return true
			}

			// Try redistribution
			if deletionCount < maxDeletions {
				logger.Info("CoreDNS pods on same node type, attempting redistribution",
					"hybridPods", hybridNodes, "cloudPods", cloudNodes, "deletionAttempt", deletionCount+1)
				if deleteCoreDNSPod(ctx, k8s, logger) == nil {
					deletionCount++
				}
			}

			return false
		},
		func(opts *ik8s.ListOptions) {
			opts.LabelSelector = "k8s-app=kube-dns"
		})

	return err
}

// deleteCoreDNSPod deletes the first CoreDNS pod to trigger rescheduling
func deleteCoreDNSPod(ctx context.Context, k8s kubernetes.Interface, logger logr.Logger) error {
	pods, err := k8s.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{
		LabelSelector: "k8s-app=kube-dns",
	})
	if err != nil || len(pods.Items) == 0 {
		return fmt.Errorf("no CoreDNS pods found to delete")
	}

	// Delete the first pod
	podName := pods.Items[0].Name
	err = k8s.CoreV1().Pods("kube-system").Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete CoreDNS pod %s: %w", podName, err)
	}

	logger.Info("Deleted CoreDNS pod for redistribution", "podName", podName)
	return nil
}
