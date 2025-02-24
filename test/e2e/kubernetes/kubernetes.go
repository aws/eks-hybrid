package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

	"github.com/aws/eks-hybrid/test/e2e/constants"
)

const (
	nodePodWaitTimeout     = 3 * time.Minute
	nodePodDelayInterval   = 5 * time.Second
	daemonSetWaitTimeout   = 3 * time.Minute
	daemonSetDelayInternal = 5 * time.Second
	MinimumVersion         = "1.25"
)

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

	_, err := k8s.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating the test pod: %w", err)
	}

	err = waitForPodToBeRunning(ctx, k8s, podName, namespace, nodeName, logger)
	if err != nil {
		return fmt.Errorf("waiting for test pod to be running: %w", err)
	}
	return nil
}

func waitForPodToBeRunning(ctx context.Context, k8s kubernetes.Interface, name, namespace, nodeName string, logger logr.Logger) error {
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
	err = retry.OnError(retry.DefaultRetry, func(err error) bool {
		// Retry any error type
		return true
	}, func() error {
		var err error
		stdout, stderr, err = execPod(ctx, config, k8s, name, namespace, cmd...)
		return err
	})

	return stdout, stderr, err
}

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
	if err != nil {
		return "", "", err
	}

	return stdoutBuf.String(), stderrBuf.String(), nil
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
}
