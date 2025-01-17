package kubernetes

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	testPodNamespace = "default"
)

// VerifyNode checks that a node is healthy, can run pods, extract logs and run commands on them.
type VerifyNode struct {
	ClientConfig *rest.Config
	K8s          *clientgo.Clientset
	Logger       logr.Logger
	Region       string

	NodeIPAddress string
}

func (t VerifyNode) Run(ctx context.Context) error {
	// get the hybrid node registered using nodeadm by the internal IP of an EC2 Instance
	node, err := WaitForNode(ctx, t.K8s, t.NodeIPAddress, t.Logger)
	if err != nil {
		return err
	}
	if node == nil {
		return fmt.Errorf("returned node is nil")
	}

	nodeName := node.Name

	t.Logger.Info("Waiting for hybrid node to be ready...")
	if err = WaitForHybridNodeToBeReady(ctx, t.K8s, nodeName, t.Logger); err != nil {
		return err
	}

	t.Logger.Info("Creating a test pod on the hybrid node...")
	podName := GetNginxPodName(nodeName)
	if err = CreateNginxPodInNode(ctx, t.K8s, nodeName, testPodNamespace, t.Region, t.Logger); err != nil {
		return err
	}
	t.Logger.Info(fmt.Sprintf("Pod %s created and running on node %s", podName, nodeName))

	t.Logger.Info("Exec-ing nginx -version", "pod", podName)
	stdout, stderr, err := ExecPodWithRetries(ctx, t.ClientConfig, t.K8s, podName, testPodNamespace, "/sbin/nginx", "-version")
	if err != nil {
		return err
	}
	if !strings.Contains(stdout, "nginx") {
		return fmt.Errorf("pod exec stdout does not contain expected value %s: %s", stdout, "nginx")
	}
	if stderr != "" {
		return fmt.Errorf("pod exec stderr should be empty %s", stderr)
	}
	t.Logger.Info("Successfully exec'd nginx -version", "pod", podName)

	t.Logger.Info("Checking logs for nginx output", "pod", podName)
	logs, err := GetPodLogsWithRetries(ctx, t.K8s, podName, testPodNamespace)
	if err != nil {
		return err
	}
	if !strings.Contains(logs, "nginx") {
		return fmt.Errorf("pod log does not contain expected value %s: %s", logs, "nginx")
	}
	t.Logger.Info("Successfully validated log output", "pod", podName)

	t.Logger.Info("Deleting test pod", "pod", podName)
	if err = DeletePod(ctx, t.K8s, podName, testPodNamespace); err != nil {
		return err
	}
	t.Logger.Info("Pod deleted successfully", "pod", podName)

	return nil
}
