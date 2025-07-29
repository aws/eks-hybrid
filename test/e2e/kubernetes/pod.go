package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
	"github.com/aws/eks-hybrid/test/e2e/constants"
)

const (
	nodePodDelayInterval = 5 * time.Second
	nodePodWaitTimeout   = 3 * time.Minute
)

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

// WaitForPodsToBeRunning waits until a pod is in running phase and all containers are ready.
// It will return an error if any of the pods have already exited.
func WaitForPodsToBeRunning(ctx context.Context, k8s kubernetes.Interface, listOptions metav1.ListOptions, namespace string, logger logr.Logger) error {
	pods, err := ik8s.ListAndWait(ctx, nodePodWaitTimeout, k8s.CoreV1().Pods(namespace), func(pods *corev1.PodList) bool {
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

func WaitForDaemonSetPodToBeRunning(ctx context.Context, k8s kubernetes.Interface, namespace, daemonSetName, nodeName string, logger logr.Logger) error {
	listOptions := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", "app.kubernetes.io/name", daemonSetName),
		FieldSelector: fmt.Sprintf("%s=%s", "spec.nodeName", nodeName),
	}
	return WaitForPodsToBeRunning(ctx, k8s, listOptions, namespace, logger)
}
