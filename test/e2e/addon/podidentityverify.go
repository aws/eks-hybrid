package addon

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

const (
	getAddonTimeout      = 2 * time.Minute
	podIdentityDaemonSet = "eks-pod-identity-agent-hybrid"
	podIdentityToken     = "eks-pod-identity-token"
	policyName           = "pod-identity-association-role-policy"
	PodIdentityS3Bucket  = "PodIdentityS3Bucket"
)

type VerifyPodIdentityAddon struct {
	Cluster   string
	NodeIP    string
	K8S       *clientgo.Clientset
	EKSClient *eks.Client
	IAMClient *iam.Client
	S3Client  *s3.Client
	Logger    logr.Logger
	K8SConfig *rest.Config
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
		return err
	}

	v.Logger.Info("Check if daemon set exists", "daemonSet", podIdentityDaemonSet)
	if _, err := kubernetes.GetDaemonSet(ctx, v.Logger, v.K8S, "kube-system", podIdentityDaemonSet); err != nil {
		return err
	}

	bucket, err := getPodIdentityS3Bucket(ctx, v.Cluster, v.S3Client)
	if err != nil {
		return err
	}
	v.Logger.Info("Get S3 Bucket for pod identity add-on test", "S3 bucket", bucket)

	// Deploy a pod with service account then run aws cli to access aws resources
	node, err := kubernetes.WaitForNode(ctx, v.K8S, v.NodeIP, v.Logger)
	if err != nil {
		return err
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
					Image: "public.ecr.aws/aws-cli/aws-cli",
					Command: []string{
						"/bin/bash",
						"-c",
						"sleep infinity",
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
	if err = kubernetes.CreatePod(ctx, v.K8S, pod, v.Logger); err != nil {
		return err
	}

	execCommand := []string{
		"bash", "-c", fmt.Sprintf("aws s3 cp s3://%s/%s . > /dev/null && cat ./%s", bucket, bucketObjectKey, bucketObjectKey),
	}
	stdout, _, err := kubernetes.ExecPodWithRetries(ctx, v.K8SConfig, v.K8S, podName, namespace, execCommand...)
	if err != nil {
		return err
	}
	v.Logger.Info("Check output from exec pod with command", "output", stdout)

	if stdout != bucketObjectContent {
		return fmt.Errorf("failed to get object %s from S3 bucket %s", bucketObjectKey, bucket)
	}

	return nil
}

func getPodIdentityS3Bucket(ctx context.Context, cluster string, client *s3.Client) (string, error) {
	listBucketsOutput, err := client.ListBuckets(ctx, &s3.ListBucketsInput{
		Prefix: aws.String("ekshybridci-arch-"),
	})
	if err != nil {
		return "", err
	}

	for _, bucket := range listBucketsOutput.Buckets {
		getBucketTaggingOutput, err := client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
			Bucket: bucket.Name,
		})
		if err != nil {
			// Ignore if there is an error while retrieving tags
			continue
		}

		var foundClusterTag, foundPodIdentityTag bool
		for _, tag := range getBucketTaggingOutput.TagSet {
			if *tag.Key == "Nodeadm-E2E-Tests-Cluster" && *tag.Value == cluster {
				foundClusterTag = true
			}

			if *tag.Key == "aws:cloudformation:logical-id" && *tag.Value == PodIdentityS3Bucket {
				foundPodIdentityTag = true
			}

			if foundClusterTag && foundPodIdentityTag {
				return *bucket.Name, nil
			}
		}
	}
	return "", fmt.Errorf("S3 bucket for pod identity not found")
}
