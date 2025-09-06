package addon

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	e2errors "github.com/aws/eks-hybrid/test/e2e/errors"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
	peeredtypes "github.com/aws/eks-hybrid/test/e2e/peered/types"
)

const (
	s3CSIDriver               = "aws-mountpoint-s3-csi-driver"
	s3CSIDriverNamespace      = "kube-system"
	s3CSIController           = "s3-csi-controller"
	s3CSINode                 = "s3-csi-node"
	s3CSIDriverServiceAccount = "s3-csi-driver-sa"
	s3CSIWaitTimeout          = 5 * time.Minute
	s3TestPod                 = "s3-app"
	s3PathPrefix              = "s3-csi-test"
	s3TestMsg                 = "Hello from the container!"
)

//go:embed testdata/s3_csi_static_provisioning.yaml
var staticProvisioningYaml string

// S3MountpointCSIDriverTest tests the S3 mountpoint CSI driver addon
type S3MountpointCSIDriverTest struct {
	Cluster            string
	addon              *Addon
	K8S                peeredtypes.K8s
	EKSClient          *eks.Client
	S3Client           *s3.Client
	K8SConfig          *rest.Config
	Logger             logr.Logger
	PodIdentityRoleArn string
	Bucket             string
}

// Create installs the S3 mountpoint CSI driver addon
func (s *S3MountpointCSIDriverTest) Create(ctx context.Context) error {
	s.addon = &Addon{
		Cluster:   s.Cluster,
		Namespace: s3CSIDriverNamespace,
		Name:      s3CSIDriver,
		Version:   "v2.0.0-eksbuild.1", // need to specify v2 version explicitly for now since v1 is the default version to install
	}

	// Create pod identity association for the addon's service account
	if err := s.setupPodIdentity(ctx); err != nil {
		return fmt.Errorf("failed to setup Pod Identity for S3 CSI driver: %v", err)
	}

	if err := s.addon.CreateAndWaitForActive(ctx, s.EKSClient, s.K8S, s.Logger); err != nil {
		return err
	}

	// TODO: remove the following call once the addon is updated to work with hybrid nodes
	// Remove anti affinity to allow s3-csi-node to be deployed to hybrid nodes
	if err := kubernetes.RemoveDaemonSetAntiAffinity(ctx, s.Logger, s.K8S, s3CSIDriverNamespace, s3CSINode); err != nil {
		return fmt.Errorf("failed to remove anti affinity: %w", err)
	}

	// Wait for CSI driver controller deployment and node daemonset to be ready
	if err := kubernetes.DeploymentWaitForReplicas(ctx, s3CSIWaitTimeout, s.K8S, s3CSIDriverNamespace, s3CSIController); err != nil {
		return fmt.Errorf("controller deployment %s not ready: %w", s3CSIController, err)
	}

	if err := kubernetes.DaemonSetWaitForReady(ctx, s.Logger, s.K8S, s3CSIDriverNamespace, s3CSINode); err != nil {
		return fmt.Errorf("node daemonset %s not ready: %w", s3CSINode, err)
	}

	return nil
}

// Validate checks if S3 mountpoint CSI driver is working correctly
func (s *S3MountpointCSIDriverTest) Validate(ctx context.Context) error {
	// Replace yaml file placeholder values
	replacer := strings.NewReplacer(
		"{{S3_PATH_PREFIX}}", s3PathPrefix,
		"{{S3_BUCKET}}", s.Bucket,
		"{{S3_TEST_POD}}", s3TestPod,
		"{{S3_TEST_MSG}}", s3TestMsg,
	)

	replacedYaml := replacer.Replace(staticProvisioningYaml)
	objs, err := kubernetes.YamlToUnstructured([]byte(replacedYaml))
	if err != nil {
		return fmt.Errorf("failed to read static provisioning yaml file: %w", err)
	}

	s.Logger.Info("Applying S3 CSI static provisioning yaml")

	if err := kubernetes.UpsertManifestsWithRetries(ctx, s.K8S, objs); err != nil {
		return fmt.Errorf("failed to deploy S3 CSI static provisioning yaml: %w", err)
	}

	podListOptions := metav1.ListOptions{
		FieldSelector: "metadata.name=" + s3TestPod,
	}

	if err := kubernetes.WaitForPodsToBeRunning(ctx, s.K8S, podListOptions, namespace, s.Logger); err != nil {
		return fmt.Errorf("failed to wait for test pod to be running: %w", err)
	}

	listObjectsOutput, err := s.S3Client.ListObjects(ctx, &s3.ListObjectsInput{
		Bucket: aws.String(s.Bucket),
		Prefix: aws.String(s3PathPrefix),
	})
	if err != nil {
		return fmt.Errorf("failed to list objects in S3 bucket: %w", err)
	}

	if len(listObjectsOutput.Contents) != 1 {
		return fmt.Errorf("there should be only 1 object in the S3 bucket")
	}

	s.Logger.Info("Validating S3 object contains test message")
	obj := listObjectsOutput.Contents[0]

	body, err := s.getS3ObjectContent(ctx, s.Bucket, *obj.Key)
	if err != nil {
		return fmt.Errorf("failed to get object content from S3 bucket: %w", err)
	}

	if !strings.Contains(string(body), s3TestMsg) {
		return fmt.Errorf("S3 object does not contain expected message: %s", s3TestMsg)
	}

	// Clean up - delete static provisioning yaml
	if err := kubernetes.DeleteManifestsWithRetries(ctx, s.K8S, objs); err != nil {
		return fmt.Errorf("failed to delete S3 CSI static provisioning yaml: %w", err)
	}

	// Clean up S3 object
	_, err = s.S3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    obj.Key,
	})
	if err != nil {
		return fmt.Errorf("failed to delete object from S3 bucket: %w", err)
	}

	return nil
}

func (s *S3MountpointCSIDriverTest) Delete(ctx context.Context) error {
	return s.addon.Delete(ctx, s.EKSClient, s.Logger)
}

func (s *S3MountpointCSIDriverTest) setupPodIdentity(ctx context.Context) error {
	s.Logger.Info("Setting up Pod Identity for S3 CSI driver")

	// Create Pod Identity Association for the addon's service account
	createAssociationInput := &eks.CreatePodIdentityAssociationInput{
		ClusterName:    aws.String(s.Cluster),
		Namespace:      aws.String(s3CSIDriverNamespace),
		RoleArn:        aws.String(s.PodIdentityRoleArn),
		ServiceAccount: aws.String(s3CSIDriverServiceAccount),
	}

	createAssociationOutput, err := s.EKSClient.CreatePodIdentityAssociation(ctx, createAssociationInput)

	if err != nil && e2errors.IsType(err, &ekstypes.ResourceInUseException{}) {
		s.Logger.Info("Pod Identity Association already exists for service account", "serviceAccount", s3CSIDriverServiceAccount)
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to create Pod Identity Association: %v", err)
	}

	s.Logger.Info("Created Pod Identity Association", "associationID", *createAssociationOutput.Association.AssociationId)
	return nil
}

func (s *S3MountpointCSIDriverTest) getS3ObjectContent(ctx context.Context, bucket, key string) ([]byte, error) {
	getObjectOutput, err := s.S3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get object [%s] from S3 bucket [%s]: %w", key, bucket, err)
	}

	defer getObjectOutput.Body.Close()
	body, err := io.ReadAll(getObjectOutput.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read object body: %w", err)
	}

	return body, nil
}
