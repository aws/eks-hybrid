package addon

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

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
)

//go:embed testdata/fsx_csi_dynamic_provisioning.yaml
var fsxDynamicProvisioningYaml string

// AWSFSXCSIDriverTest tests the AWS FSX CSI driver addon
type FsxCSIDriverTest struct {
	Cluster            string
	addon              *Addon
	K8S                peeredtypes.K8s
	EKSClient          *eks.Client
	K8SConfig          *rest.Config
	Logger             logr.Logger
	PodIdentityRoleArn string
	SubnetID           string
	SecurityGroupID    string
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
	// Generate unique suffix to avoid resource name collisions
	uniqueSuffix := fmt.Sprintf("-%d", time.Now().Unix())
	uniqueTestPod := fsxTestPod + uniqueSuffix

	// Replace yaml file placeholder values
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

	// Try to read the output file
	execCmd := []string{"cat", "/data/out.txt"}
	stdout, stderr, err := kubernetes.ExecPodWithRetries(ctx, f.K8SConfig, f.K8S, uniqueTestPod, defaultNamespace, execCmd...)
	if err != nil {
		return fmt.Errorf("could not read data from FSX volume: %w", err)
	}

	if stderr != "" {
		return fmt.Errorf("stderr is not empty: %s", stderr)
	}

	if strings.TrimSpace(stdout) != fsxTestString {
		return fmt.Errorf("expected string value %s, got %s", fsxTestString, strings.TrimSpace(stdout))
	}

	f.Logger.Info("FSx CSI Driver validation successful")

	// Clean up - delete dynamic provisioning yaml
	if err := kubernetes.DeleteManifestsWithRetries(ctx, f.K8S, objs); err != nil {
		return fmt.Errorf("failed to delete FSX CSI dynamic provisioning yaml: %w", err)
	}

	f.Logger.Info("FSx test resources cleaned up successfully")

	return nil
}

func (f *FsxCSIDriverTest) Delete(ctx context.Context) error {
	return f.addon.Delete(ctx, f.EKSClient, f.Logger)
}
