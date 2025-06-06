package nvidia

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	ik8s "github.com/aws/eks-hybrid/internal/kubernetes"
	"github.com/aws/eks-hybrid/test/e2e/commands"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

type DevicePluginTest struct {
	Cluster       string
	K8S           clientgo.Interface
	EKSClient     *eks.Client
	K8SConfig     *rest.Config
	Logger        logr.Logger
	CommandRunner commands.RemoteCommandRunner

	NumberOfNodes int
}

const (
	nodeWaitTimeout          = 5 * time.Minute
	nvidiaDriverWaitTimeout  = 20 * time.Minute
	nvidiaDriverWaitInterval = 1 * time.Minute
)

// WaitForNvidiaDrivers checks if nvidia-smi command succeeds on the node
func (d *DevicePluginTest) WaitForNvidiaDriverReady(ctx context.Context) error {
	nodes, err := ik8s.ListAndWait(ctx, nodeWaitTimeout, d.K8S.CoreV1().Nodes(), func(nodes *corev1.NodeList) bool {
		return len(nodes.Items) == d.NumberOfNodes
	})
	if err != nil {
		return fmt.Errorf("Number of nodes in Kubernetes does not match the expected number of nodes: %w", err)
	}

	for _, node := range nodes.Items {
		ip := kubernetes.GetNodeInternalIP(&node)
		if ip == "" {
			return fmt.Errorf("failed to get internal IP for node %s", node.Name)
		}

		err := wait.PollUntilContextTimeout(ctx, nvidiaDriverWaitInterval, nvidiaDriverWaitTimeout, true, func(ctx context.Context) (bool, error) {
			if commandOutput, err := d.CommandRunner.Run(ctx, ip, []string{"nvidia-smi"}); err != nil || commandOutput.ResponseCode != 0 {
				d.Logger.Info("nvidia-smi command failed", "node", node.Name, "error", err, "responseCode", commandOutput.ResponseCode)
				return false, nil
			}
			return true, nil
		})
		if err != nil {
			return fmt.Errorf("nvidia-smi command failed on node %s: %w", node.Name, err)
		}
	}

	return nil
}
