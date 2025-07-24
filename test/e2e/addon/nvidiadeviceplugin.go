package addon

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	"github.com/aws/eks-hybrid/test/e2e/commands"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
	peeredtypes "github.com/aws/eks-hybrid/test/e2e/peered/types"
)

type NvidiaDevicePluginTest struct {
	Cluster       string
	K8S           peeredtypes.K8s
	EKSClient     *eks.Client
	K8SConfig     *rest.Config
	Logger        logr.Logger
	Command       string
	CommandRunner commands.RemoteCommandRunner

	NodeName string
}

const (
	nodeWaitTimeout          = 5 * time.Minute
	nvidiaDriverWaitTimeout  = 20 * time.Minute
	nvidiaDriverWaitInterval = 1 * time.Minute
	testPodName              = "gpu-pod"
)

//go:embed testdata/nvidia-device-plugin-v0.17.1.yaml
var devicePluginYaml []byte

//go:embed testdata/gpu-pod.yaml
var gpuPodYaml []byte

// WaitForNvidiaDrivers checks if nvidia-smi command succeeds on the node
func (n *NvidiaDevicePluginTest) WaitForNvidiaDriverReady(ctx context.Context) error {
	node, err := kubernetes.WaitForNode(ctx, n.K8S, n.NodeName, n.Logger)
	if err != nil {
		return fmt.Errorf("failed to get node %s: %w", n.NodeName, err)
	}

	ip := kubernetes.GetNodeInternalIP(node)
	if ip == "" {
		return fmt.Errorf("failed to get internal IP for node %s", node.Name)
	}

	err = wait.PollUntilContextTimeout(ctx, nvidiaDriverWaitInterval, nvidiaDriverWaitTimeout, true, func(ctx context.Context) (bool, error) {
		if commandOutput, err := n.CommandRunner.Run(ctx, ip, []string{n.Command}); err != nil || commandOutput.ResponseCode != 0 {
			n.Logger.Info("nvidia-smi command failed", "node", node.Name, "error", err, "responseCode", commandOutput.ResponseCode)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("nvidia-smi command failed on node %s: %w", node.Name, err)
	}

	return nil
}

func (n *NvidiaDevicePluginTest) Create(ctx context.Context) error {
	objs, err := kubernetes.YamlToUnstructured(devicePluginYaml)
	if err != nil {
		return fmt.Errorf("failed to read device plugin yaml file: %w", err)
	}

	n.Logger.Info("Applying device plugin yaml")

	if err := kubernetes.UpsertManifestsWithRetries(ctx, n.K8S, objs); err != nil {
		return fmt.Errorf("failed to deploy device plugin: %w", err)
	}
	return nil
}

func (n *NvidiaDevicePluginTest) Validate(ctx context.Context) error {
	objs, err := kubernetes.YamlToUnstructured(gpuPodYaml)
	if err != nil {
		return fmt.Errorf("failed to read gpu yaml file: %w", err)
	}

	n.Logger.Info("Applying gpu pod yaml")

	if err := kubernetes.UpsertManifestsWithRetries(ctx, n.K8S, objs); err != nil {
		return fmt.Errorf("failed to deploy gpu pod: %w", err)
	}

	if err := kubernetes.WaitForPodToBeCompleted(ctx, n.K8S, testPodName, namespace); err != nil {
		return fmt.Errorf("failed to wait for gpu pod to be completed: %w", err)
	}

	logs, err := kubernetes.FetchLogs(ctx, n.K8S, testPodName, namespace)
	if err != nil {
		return fmt.Errorf("failed to fetch logs for gpu pod: %w", err)
	}

	if !strings.Contains(logs, "Test PASSED") {
		return fmt.Errorf("gpu pod test failed: %s", logs)
	}

	if err := kubernetes.DeleteManifestsWithRetries(ctx, n.K8S, objs); err != nil {
		return fmt.Errorf("failed to delete gpu pod: %w", err)
	}

	return nil
}

func (n *NvidiaDevicePluginTest) Delete(ctx context.Context) error {
	objs, err := kubernetes.YamlToUnstructured(devicePluginYaml)
	if err != nil {
		return fmt.Errorf("failed to read device plugin yaml file: %w", err)
	}

	n.Logger.Info("Deleting device plugin yaml")
	if err := kubernetes.DeleteManifestsWithRetries(ctx, n.K8S, objs); err != nil {
		return fmt.Errorf("failed to delete device plugin: %w", err)
	}

	return nil
}
