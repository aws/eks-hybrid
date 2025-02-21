package kubernetes

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	testPodNamespace = "default"
)

// VerifyNode checks that a node is healthy, can run pods, extract logs and run commands on them.
type VerifyNode struct {
	ClientConfig *rest.Config
	K8s          clientgo.Interface
	Logger       logr.Logger
	Region       string

	NodeIPAddress string
}

func (v VerifyNode) WaitForNodeReady(ctx context.Context) (*corev1.Node, error) {
	// get the hybrid node registered using nodeadm by the internal IP of an EC2 Instance
	node, err := WaitForNode(ctx, v.K8s, v.NodeIPAddress, v.Logger)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, fmt.Errorf("returned node is nil")
	}

	nodeName := node.Name

	v.Logger.Info("Waiting for hybrid node to be ready...")
	if err = WaitForHybridNodeToBeReady(ctx, v.K8s, nodeName, v.Logger); err != nil {
		return nil, err
	}

	return node, nil
}

func (v VerifyNode) Run(ctx context.Context) error {
	node, err := v.WaitForNodeReady(ctx)
	if err != nil {
		return err
	}

	nodeName := node.Name

	v.Logger.Info("Creating a test pod on the hybrid node...")
	podName := GetNginxPodName(nodeName)
	if err = CreateNginxPodInNode(ctx, v.K8s, nodeName, testPodNamespace, v.Region, v.Logger); err != nil {
		return err
	}
	v.Logger.Info(fmt.Sprintf("Pod %s created and running on node %s", podName, nodeName))

	v.Logger.Info("Exec-ing nginx -version", "pod", podName)
	stdout, stderr, err := ExecPodWithRetries(ctx, v.ClientConfig, v.K8s, podName, testPodNamespace, "/sbin/nginx", "-version")
	if err != nil {
		return err
	}
	if !strings.Contains(stdout, "nginx") {
		return fmt.Errorf("pod exec stdout does not contain expected value %s: %s", stdout, "nginx")
	}
	if stderr != "" {
		return fmt.Errorf("pod exec stderr should be empty %s", stderr)
	}
	v.Logger.Info("Successfully exec'd nginx -version", "pod", podName)

	v.Logger.Info("Checking logs for nginx output", "pod", podName)
	logs, err := GetPodLogsWithRetries(ctx, v.K8s, podName, testPodNamespace)
	if err != nil {
		return err
	}
	if !strings.Contains(logs, "nginx") {
		return fmt.Errorf("pod log does not contain expected value %s: %s", logs, "nginx")
	}
	v.Logger.Info("Successfully validated log output", "pod", podName)

	v.Logger.Info("Deleting test pod", "pod", podName)
	if err = DeletePod(ctx, v.K8s, podName, testPodNamespace); err != nil {
		return err
	}
	v.Logger.Info("Pod deleted successfully", "pod", podName)

	return nil
}
