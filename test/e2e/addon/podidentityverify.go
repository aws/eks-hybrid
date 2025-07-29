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
	getAddonTimeout           = 10 * time.Minute
	podIdentityToken          = "eks-pod-identity-token"
	policyName                = "pod-identity-association-role-policy"
	PodIdentityS3BucketPrefix = "podid"
)

type VerifyPodIdentityAddon struct {
	Cluster             string
	NodeName            string
	PodIdentityS3Bucket string
	K8S                 clientgo.Interface
	EKSClient           *eks.Client
	IAMClient           *iam.Client
	S3Client            *s3.Client
	Logger              logr.Logger
	K8SConfig           *rest.Config
	Region              string
}

func (v VerifyPodIdentityAddon) Run(ctx context.Context) error {
	v.Logger.Info("Verify pod identity add-on is installed")

	podIdentityAddon := Addon{
		Name:    podIdentityAgent,
		Cluster: v.Cluster,
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, getAddonTimeout)
	defer cancel()

	if err := podIdentityAddon.WaitUntilActive(timeoutCtx, v.EKSClient, v.Logger); err != nil {
		return fmt.Errorf("waiting for pod identity add-on to be active: %w", err)
	}

	node, err := kubernetes.WaitForNode(ctx, v.K8S, v.NodeName, v.Logger)
	if err != nil {
		return fmt.Errorf("waiting for node %s to be ready: %w", v.NodeName, err)
	}

	v.Logger.Info("Looking for pod identity pod on target node", "nodeName", node.Name)
	if err := v.waitForPodIdentityPodOnNode(ctx, node.Name); err != nil {
		return fmt.Errorf("waiting for pod identity pod to be running on node %s: %w", node.Name, err)
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
					Env: []corev1.EnvVar{
						// default value for AWS_MAX_ATTEMPTS is 3. We are seeing the s3 cp command
						// fail due to rate limits form additional tests so increasing the number of retries
						{
							Name:  "AWS_MAX_ATTEMPTS",
							Value: "10",
						},
					},
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

	defer func() {
		if err := kubernetes.DeletePod(ctx, v.K8S, podName, namespace); err != nil {
			// it's okay not fail this operation as the pod would be eventually deleted when the cluster is deleted
			v.Logger.Info("Fail to delete aws pod", "podName", podName, "error", err)
		}
	}()

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

	return nil
}

func (v VerifyPodIdentityAddon) waitForPodIdentityPodOnNode(ctx context.Context, nodeName string) error {
	v.Logger.Info("Waiting for pod identity pod on node", "nodeName", nodeName)

	listOptions := metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/instance=eks-pod-identity-agent",
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	}

	return kubernetes.WaitForPodsToBeRunning(ctx, v.K8S, listOptions, "kube-system", v.Logger)
}
