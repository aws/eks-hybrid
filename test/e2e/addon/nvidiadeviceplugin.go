package addon

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/aws/eks-hybrid/test/e2e/commands"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

type NvidiaDevicePluginTest struct {
	Cluster       string
	K8S           clientgo.Interface
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
)

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
