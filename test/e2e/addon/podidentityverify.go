package addon

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/aws/eks-hybrid/test/e2e/constants"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

const (
	getAddonTimeout           = 2 * time.Minute
	podIdentityDaemonSet      = "eks-pod-identity-agent-hybrid"
	podIdentityToken          = "eks-pod-identity-token"
	policyName                = "pod-identity-association-role-policy"
	PodIdentityS3Bucket       = "PodIdentityS3Bucket"
	PodIdentityS3BucketPrefix = "podidentitys3bucket"
)

type VerifyPodIdentityAddon struct {
	Cluster             string
	NodeIP              string
	PodIdentityS3Bucket string
	K8S                 clientgo.Interface
	EKSClient           *eks.Client
	IAMClient           *iam.Client
	S3Client            *s3.Client
	Logger              logr.Logger
	K8SConfig           *rest.Config
	Region              string
}

type PolicyDocument struct {
	Version   string
	Statement []StatementEntry
}

type StatementEntry struct {
	Effect   string
	Action   []string
	Resource []string
}

func (v VerifyPodIdentityAddon) Run(ctx context.Context) error {
	v.Logger.Info("Verify pod identity add-on is installed")

	podIdentityAddon := Addon{
		Name:    podIdentityAgent,
		Cluster: v.Cluster,
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, getAddonTimeout)
	defer cancel()

	if err := podIdentityAddon.WaitUtilActive(timeoutCtx, v.EKSClient, v.Logger); err != nil {
		return fmt.Errorf("waiting for pod identity add-on to be active: %w", err)
	}

	v.Logger.Info("Check if daemon set exists", "daemonSet", podIdentityDaemonSet)
	if _, err := kubernetes.GetDaemonSet(ctx, v.Logger, v.K8S, "kube-system", podIdentityDaemonSet); err != nil {
		return fmt.Errorf("getting daemon set %s: %w", podIdentityDaemonSet, err)
	}

	node, err := kubernetes.WaitForNode(ctx, v.K8S, v.NodeIP, v.Logger)
	if err != nil {
		return fmt.Errorf("waiting for node %s to be ready: %w", v.NodeIP, err)
	}

	if err := kubernetes.WaitForDaemonSetPodToBeRunning(ctx, v.K8S, "kube-system", podIdentityDaemonSet, node.Name, v.Logger); err != nil {
		return fmt.Errorf("waiting for pod identity daemon set to be running on pod: %w", err)
	}

	podName := fmt.Sprintf("awscli-%s", node.Name)
	v.Logger.Info("Creating a test pod on the hybrid node for pod identity add-on to access aws resources")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  podName,
					Image: fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com/ecr-public/aws-cli/aws-cli:latest", constants.EcrAccounId, v.Region),
					Command: []string{
						"/bin/bash",
						"-c",
						"sleep infinity",
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							Exec: &corev1.ExecAction{
								Command: []string{
									"bash", "-c", "aws sts get-caller-identity",
								},
							},
						},
						// default value for initialDelaySeconds is 0 and for periodSeconds is 10
						// it would fail readiness probe after 5 failures (50 seconds)
						FailureThreshold: 5,
						TimeoutSeconds:   20,
						PeriodSeconds:    20,
					},
				},
			},
			// schedule the pod on the specific node using nodeSelector
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": node.Name,
			},
			ServiceAccountName: podIdentityServiceAccount,
		},
	}

	// Deploy a pod with service account then run aws cli to access aws resources
	if err = kubernetes.CreatePod(ctx, v.K8S, pod, v.Logger); err != nil {
		return fmt.Errorf("creating the awscli pod %s: %w", podName, err)
	}

	execCommand := []string{
		"bash", "-c", fmt.Sprintf("aws s3 cp s3://%s/%s . > /dev/null && cat ./%s", v.PodIdentityS3Bucket, bucketObjectKey, bucketObjectKey),
	}
	stdout, stdErr, err := kubernetes.ExecPodWithRetries(ctx, v.K8SConfig, v.K8S, podName, namespace, execCommand...)
	if err != nil {
		return fmt.Errorf("exec aws s3 cp command on pod %s: err: %w, stdout: %s, stderr: %s", podName, err, stdout, stdErr)
	}

	if stdout != bucketObjectContent {
		return fmt.Errorf("getting object %s from S3 bucket %s", bucketObjectKey, v.PodIdentityS3Bucket)
	}

	if err := kubernetes.DeletePod(ctx, v.K8S, podName, namespace); err != nil {
		return fmt.Errorf("deleting the awscli pod %s: %w", podName, err)
	}

	return nil
}
