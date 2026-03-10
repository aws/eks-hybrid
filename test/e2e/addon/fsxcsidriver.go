package addon

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/fsx"
	fsxtypes "github.com/aws/aws-sdk-go-v2/service/fsx/types"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	"github.com/aws/eks-hybrid/test/e2e/constants"
	e2eerrors "github.com/aws/eks-hybrid/test/e2e/errors"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
	peeredtypes "github.com/aws/eks-hybrid/test/e2e/peered/types"
)

const (
	fsxCSIDriver                = "aws-fsx-csi-driver"
	fsxCSIDriverNamespace       = "kube-system"
	fsxTestPod                  = "fsx-test-app"
	fsxControllerServiceAccount = "fsx-csi-controller-sa"
	fsxTestString               = "Hello FSX CSI Driver"
	fsxPodWaitTimeout           = 35 * time.Minute
	fsxDeletionTimeout          = 15 * time.Minute
	fsxDeletionPollInterval     = 30 * time.Second
)

//go:embed testdata/fsx_csi_dynamic_provisioning.yaml
var fsxDynamicProvisioningYaml string

// FsxCSIDriverTest tests the AWS FSX CSI driver addon
type FsxCSIDriverTest struct {
	Cluster            string
	addon              *Addon
	K8S                peeredtypes.K8s
	EKSClient          *eks.Client
	FSXClient          *fsx.Client
	K8SConfig          *rest.Config
	Logger             logr.Logger
	PodIdentityRoleArn string
	SubnetID           string
	SecurityGroupID    string
	manifests          []unstructured.Unstructured
	fileSystemID       string
}

// Create installs the AWS FSX CSI driver addon
func (f *FsxCSIDriverTest) Create(ctx context.Context) error {
	f.addon = &Addon{
		Cluster:   f.Cluster,
		Namespace: fsxCSIDriverNamespace,
		Name:      fsxCSIDriver,
		PodIdentityAssociations: []PodIdentityAssociation{
			{
				RoleArn:        f.PodIdentityRoleArn,
				ServiceAccount: fsxControllerServiceAccount,
			},
		},
	}

	f.Logger.Info("Creating AWS FSX CSI driver addon (assuming success for hybrid nodes)")

	if err := f.addon.Create(ctx, f.EKSClient, f.Logger); err != nil {
		return fmt.Errorf("failed to create AWS FSX CSI driver addon: %w", err)
	}

	f.Logger.Info("AWS FSX CSI driver addon created successfully")
	return nil
}

// Validate checks if AWS FSX CSI driver is working correctly
func (f *FsxCSIDriverTest) Validate(ctx context.Context) error {
	uniqueSuffix := fmt.Sprintf("-%d", time.Now().Unix())
	uniqueTestPod := fsxTestPod + uniqueSuffix
	pvcName := "fsx-claim" + uniqueSuffix

	replacer := strings.NewReplacer(
		"{{NAMESPACE}}", defaultNamespace,
		"{{FSX_TEST_POD}}", uniqueTestPod,
		"{{SUBNET_ID}}", f.SubnetID,
		"{{SECURITY_GROUP_ID}}", f.SecurityGroupID,
		"{{FSX_TEST_STRING}}", fsxTestString,
		"{{UNIQUE_SUFFIX}}", uniqueSuffix,
	)

	replacedYaml := replacer.Replace(fsxDynamicProvisioningYaml)
	objs, err := kubernetes.YamlToUnstructured([]byte(replacedYaml))
	if err != nil {
		return fmt.Errorf("failed to read FSX CSI dynamic provisioning yaml file: %w", err)
	}

	f.manifests = objs

	f.Logger.Info("Applying FSX CSI dynamic provisioning yaml")

	if err := kubernetes.UpsertManifestsWithRetries(ctx, f.K8S, objs); err != nil {
		return fmt.Errorf("failed to deploy FSX CSI dynamic provisioning yaml: %w", err)
	}

	podListOptions := metav1.ListOptions{
		FieldSelector: "metadata.name=" + uniqueTestPod,
	}

	if err := kubernetes.WaitForPodsToBeRunningWithTimeout(ctx, f.K8S, podListOptions, defaultNamespace, f.Logger, fsxPodWaitTimeout); err != nil {
		return fmt.Errorf("failed to wait for test pod to be running: %w", err)
	}

	f.Logger.Info("FSx test pod is running successfully", "podName", uniqueTestPod)

	if err := f.tagFileSystem(ctx, pvcName); err != nil {
		f.Logger.Error(err, "Failed to tag FSx file system")
	}

	execCmd := []string{"head", "-1", "/data/out.txt"}
	stdout, stderr, err := kubernetes.ExecPodWithRetries(ctx, f.K8SConfig, f.K8S, uniqueTestPod, defaultNamespace, execCmd...)
	if err != nil {
		return fmt.Errorf("could not read data from FSX volume: %w", err)
	}

	if stderr != "" {
		return fmt.Errorf("stderr is not empty: %s", stderr)
	}

	if strings.TrimSpace(stdout) != fsxTestString {
		return fmt.Errorf("expected %q, got %q", fsxTestString, strings.TrimSpace(stdout))
	}

	f.Logger.Info("FSx CSI Driver validation successful")

	return nil
}

func (f *FsxCSIDriverTest) tagFileSystem(ctx context.Context, pvcName string) error {
	fileSystemID, err := kubernetes.CSIVolumeHandleFromPVC(ctx, f.K8S, defaultNamespace, pvcName)
	if err != nil {
		return fmt.Errorf("getting FSx file system ID from PVC: %w", err)
	}

	f.fileSystemID = fileSystemID

	output, err := f.FSXClient.DescribeFileSystems(ctx, &fsx.DescribeFileSystemsInput{
		FileSystemIds: []string{fileSystemID},
	})
	if err != nil {
		return fmt.Errorf("describing FSx file system %s: %w", fileSystemID, err)
	}
	if len(output.FileSystems) == 0 {
		return fmt.Errorf("FSx file system %s not found", fileSystemID)
	}

	resourceARN := output.FileSystems[0].ResourceARN

	now := time.Now().UTC().Format(time.RFC3339)
	f.Logger.Info("Tagging FSx file system", "fileSystemId", fileSystemID)
	_, err = f.FSXClient.TagResource(ctx, &fsx.TagResourceInput{
		ResourceARN: resourceARN,
		Tags: []fsxtypes.Tag{
			{Key: aws.String("Name"), Value: aws.String(f.Cluster + "-fsx-lustre")},
			{Key: aws.String(constants.TestClusterTagKey), Value: aws.String(f.Cluster)},
			{Key: aws.String(constants.CreationTimeTagKey), Value: aws.String(now)},
		},
	})
	if err != nil {
		return fmt.Errorf("tagging FSx file system %s: %w", fileSystemID, err)
	}

	f.Logger.Info("Tagged FSx file system successfully", "fileSystemId", fileSystemID)
	return nil
}

func (f *FsxCSIDriverTest) Delete(ctx context.Context) error {
	// Delete manifests first while the CSI controller is still running
	// so it can process the PVC deletion and delete the FSx filesystem
	if f.manifests != nil {
		f.Logger.Info("Deleting FSx test manifests")
		if err := kubernetes.DeleteManifestsWithRetries(ctx, f.K8S, f.manifests); err != nil {
			f.Logger.Error(err, "Failed to delete FSx test manifests")
		}
	}

	// Wait for the FSx filesystem to be fully deleted.
	if f.fileSystemID != "" {
		f.Logger.Info("Waiting for FSx file system to be deleted", "fileSystemId", f.fileSystemID)
		if err := f.waitForFileSystemDeletion(ctx); err != nil {
			f.Logger.Error(err, "Failed waiting for FSx file system deletion, will rely on sweeper", "fileSystemId", f.fileSystemID)
		}
	}

	return f.addon.Delete(ctx, f.EKSClient, f.Logger)
}

func (f *FsxCSIDriverTest) waitForFileSystemDeletion(ctx context.Context) error {
	return wait.PollUntilContextTimeout(ctx, fsxDeletionPollInterval, fsxDeletionTimeout, true, func(ctx context.Context) (bool, error) {
		output, err := f.FSXClient.DescribeFileSystems(ctx, &fsx.DescribeFileSystemsInput{
			FileSystemIds: []string{f.fileSystemID},
		})
		if e2eerrors.IsType(err, &fsxtypes.FileSystemNotFound{}) {
			f.Logger.Info("FSx file system deleted", "fileSystemId", f.fileSystemID)
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("describing FSx file system %s: %w", f.fileSystemID, err)
		}
		if len(output.FileSystems) == 0 {
			f.Logger.Info("FSx file system deleted", "fileSystemId", f.fileSystemID)
			return true, nil
		}

		f.Logger.Info("Waiting for FSx file system deletion", "fileSystemId", f.fileSystemID, "status", output.FileSystems[0].Lifecycle)
		return false, nil
	})
}
