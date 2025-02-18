package addon

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
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

	bucket, err := getBucketNameFromPodIdentityAssociation(ctx, v.Cluster, v.EKSClient, v.IAMClient)
	if err != nil {
		return err
	}
	v.Logger.Info("Get S3 Bucket for pod identity add-on test", "S3 bucket", bucket)

	// Deploy a pod with service account then run aws cli to access aws resources
	node, err := kubernetes.WaitForNode(ctx, v.K8S, v.NodeIP, v.Logger)
	if err != nil {
		return err
	}

	podName := "awscli"
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

func getBucketNameFromPodIdentityAssociation(ctx context.Context, cluster string, eksClient *eks.Client, iamClient *iam.Client) (string, error) {
	// The idea behind this function is
	// 1. it gets role ARN from pod identity association for a given service account,
	// 2. it retrieves policy document given role ARN and inline policy name,
	// 3. it parses policy document to get S3 bucket name

	var err error
	roleName, err := getAssociatedRoleName(ctx, cluster, eksClient)
	if err != nil {
		return "", err
	}

	getRolePolicyOutput, err := iamClient.GetRolePolicy(ctx, &iam.GetRolePolicyInput{
		RoleName:   aws.String(roleName),
		PolicyName: aws.String(policyName),
	})
	if err != nil {
		return "", err
	}

	policyDocument, err := unmarshallPolicyDocument(*getRolePolicyOutput.PolicyDocument)
	if err != nil {
		return "", err
	}

	// see policyDocument defined in setup-cfn.yaml::PodIdentityAssociationRole
	for _, resource := range policyDocument.Statement[0].Resource {
		if !strings.HasPrefix(resource, "arn:aws:s3::") {
			continue
		}

		// Sample S3 bucket ARN
		// "arn:aws:s3:::ekshybridci-arch-nodeadm-e2e-t-podidentitys3bucket-coydlns2q8if",
		// "arn:aws:s3:::ekshybridci-arch-nodeadm-e2e-t-podidentitys3bucket-coydlns2q8if/*"
		bucket := lastSegment(strings.Split(resource, "/")[0], ":")
		return bucket, nil
	}

	return "", fmt.Errorf("failed to find S3 bucket")
}

func getAssociatedRoleName(ctx context.Context, cluster string, eksClient *eks.Client) (string, error) {
	var err error
	out, err := eksClient.ListPodIdentityAssociations(ctx, &eks.ListPodIdentityAssociationsInput{
		ClusterName: &cluster,
	})
	if err != nil {
		return "", err
	}

	var associationId *string
	for _, association := range out.Associations {
		if association.Namespace == nil || *association.Namespace != namespace {
			continue
		}

		if association.ServiceAccount == nil || *association.ServiceAccount != podIdentityServiceAccount {
			continue
		}

		// there is at most one association for a service account
		associationId = association.AssociationId
		break
	}

	if associationId == nil {
		return "", fmt.Errorf("failed to find pod identity association for service account %s in namespace %s", podIdentityServiceAccount, namespace)
	}

	describePodIdentityAssociationInput := &eks.DescribePodIdentityAssociationInput{
		AssociationId: associationId,
		ClusterName:   &cluster,
	}
	if out, err := eksClient.DescribePodIdentityAssociation(ctx, describePodIdentityAssociationInput); err != nil {
		return "", err
	} else {
		// sample role ARN
		// arn:aws:iam::1234567890:role/EKSHybridCI-Arch-nodeadm-e2e-tests-ClusterRole-E0hZO1KMNC3v
		return lastSegment(*out.Association.RoleArn, "/"), nil
	}
}

func lastSegment(str, sep string) string {
	return str[strings.LastIndex(str, sep)+1:]
}

func unmarshallPolicyDocument(document string) (*PolicyDocument, error) {
	var policyDocument PolicyDocument
	documentUnencoded, err := url.QueryUnescape(document)
	if err != nil {
		return &PolicyDocument{}, err
	}
	err = json.Unmarshal([]byte(documentUnencoded), &policyDocument)
	if err != nil {
		return &PolicyDocument{}, err
	}
	return &policyDocument, nil
}
