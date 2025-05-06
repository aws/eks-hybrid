package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/retry"
	"k8s.io/kubectl/pkg/scheme"

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

	_, err := k8s.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating the test pod: %w", err)
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
func WaitForPodsToBeRunning(ctx context.Context, k8s kubernetes.Interface, listOptions metav1.ListOptions, namespace string, logger logr.Logger) error {
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

		for _, pod := range pods.Items {
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

// Retries up to 5 times to avoid connection errors
func GetPodLogsWithRetries(ctx context.Context, k8s kubernetes.Interface, name, namespace string, opts *corev1.PodLogOptions) (logs string, err error) {
	err = retry.OnError(retry.DefaultRetry, func(err error) bool {
		// Retry any error type
		return true
	}, func() error {
		var err error
		logs, err = getPodLogs(ctx, k8s, name, namespace, opts)
		return err
	})

	return logs, err
}

func getPodLogs(ctx context.Context, k8s kubernetes.Interface, name, namespace string, opts *corev1.PodLogOptions) (string, error) {
	req := k8s.CoreV1().Pods(namespace).GetLogs(name, opts)
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
