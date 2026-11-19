package addon

import (
	"context"
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

const (
	networkFlowMonitorNamespace      = "amazon-network-flow-monitor"
	networkFlowMonitorAddonName      = "aws-network-flow-monitoring-agent"
	networkFlowMonitorDaemonSet      = "aws-network-flow-monitor-agent"
	networkFlowMonitorServiceAccount = "aws-network-flow-monitor-agent-service-account"
	networkFlowMonitorLabelSelector  = "app.kubernetes.io/name=" + networkFlowMonitorDaemonSet
	networkFlowMonitorWaitTimeout    = 3 * time.Minute

	trafficGenPodName = "nfm-traffic-gen"
)

type NetworkFlowMonitorTest struct {
	AddonTestConfig
	PodIdentityRoleArn string
	addon              *Addon
}

func (n *NetworkFlowMonitorTest) Create(ctx context.Context) error {
	n.addon = &Addon{
		Cluster:   n.Cluster,
		Namespace: networkFlowMonitorNamespace,
		Name:      networkFlowMonitorAddonName,
		PodIdentityAssociations: []PodIdentityAssociation{
			{
				RoleArn:        n.PodIdentityRoleArn,
				ServiceAccount: networkFlowMonitorServiceAccount,
			},
		},
	}

	if err := n.addon.CreateAndWaitForActive(ctx, n.EKSClient, n.K8S, n.Logger); err != nil {
		return err
	}

	if err := kubernetes.DaemonSetWaitForReady(ctx, n.Logger, n.K8S, networkFlowMonitorNamespace, networkFlowMonitorDaemonSet); err != nil {
		return err
	}

	return nil
}

func (n *NetworkFlowMonitorTest) Validate(ctx context.Context) error {
	hybridNodes, err := kubernetes.ListNodesWithLabels(ctx, n.K8S, kubernetes.HybridNodeLabelSelector)
	if err != nil {
		return err
	}
	if len(hybridNodes.Items) == 0 {
		return fmt.Errorf("no hybrid nodes found in cluster")
	}

	if err := n.generateTraffic(ctx, hybridNodes.Items[0].Name); err != nil {
		return err
	}

	pods, err := kubernetes.ListPodsWithLabels(ctx, n.K8S, networkFlowMonitorNamespace, networkFlowMonitorLabelSelector)
	if err != nil {
		return err
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no network flow monitor agent pods found")
	}

	if err := n.validateAgentLogs(ctx, pods); err != nil {
		return err
	}

	return nil
}

func (n *NetworkFlowMonitorTest) generateTraffic(ctx context.Context, targetNode string) error {
	n.Logger.Info("Deploying nginx pod to generate network flows for the agent to capture", "node", targetNode)

	if err := kubernetes.CreateNginxPodInNode(ctx, n.K8S, targetNode, networkFlowMonitorNamespace, n.Region, n.Logger, n.DNSSuffix, n.EcrAccount, trafficGenPodName); err != nil {
		return fmt.Errorf("creating traffic generator pod: %v", err)
	}

	n.Logger.Info("Traffic generator pod running, nginx serving requests will create network flows")
	return nil
}

func (n *NetworkFlowMonitorTest) validateAgentLogs(ctx context.Context, pods *v1.PodList) error {
	n.Logger.Info("Checking agent logs for report publishing")

	for _, pod := range pods.Items {
		n.Logger.Info("Validating agent logs", "pod", pod.Name, "node", pod.Spec.NodeName)

		err := wait.PollUntilContextTimeout(ctx, 10*time.Second, networkFlowMonitorWaitTimeout, true, func(ctx context.Context) (bool, error) {
			logStr, err := kubernetes.GetPodLogsWithRetries(ctx, n.K8S, pod.Name, networkFlowMonitorNamespace)
			if err != nil {
				n.Logger.Info("Failed to get logs, retrying", "pod", pod.Name, "error", err)
				return false, nil
			}

			if strings.Contains(logStr, "Error getting credentials") {
				return false, fmt.Errorf("agent on pod %s has credential errors", pod.Name)
			}
			if strings.Contains(logStr, "Error sending request") {
				return false, fmt.Errorf("agent on pod %s failed to send report to endpoint", pod.Name)
			}

			// Agent logs "Publishing report" before each attempt and "status":200 after a successful publish
			if strings.Contains(logStr, "Publishing report") && strings.Contains(logStr, `"status":200`) {
				n.Logger.Info("Agent is publishing reports to endpoint",
					"pod", pod.Name,
					"node", pod.Spec.NodeName,
				)
				return true, nil
			}

			n.Logger.Info("Waiting for agent to publish reports", "pod", pod.Name)
			return false, nil
		})
		if err != nil {
			return fmt.Errorf("agent on pod %s did not publish reports within timeout: %v", pod.Name, err)
		}
	}

	return nil
}

func (n *NetworkFlowMonitorTest) PrintLogs(ctx context.Context) error {
	pods, err := kubernetes.ListPodsWithLabels(ctx, n.K8S, networkFlowMonitorNamespace, networkFlowMonitorLabelSelector)
	if err != nil {
		return fmt.Errorf("failed to list pods for %s: %v", n.addon.Name, err)
	}

	for _, pod := range pods.Items {
		logs, err := kubernetes.GetPodLogsWithRetries(ctx, n.K8S, pod.Name, pod.Namespace)
		if err != nil {
			return fmt.Errorf("failed to get logs for pod %s: %v", pod.Name, err)
		}
		n.Logger.Info("Logs for network flow monitor agent", "pod", pod.Name, "logs", logs)
	}

	return nil
}

func (n *NetworkFlowMonitorTest) Delete(ctx context.Context) error {
	_ = kubernetes.DeletePod(ctx, n.K8S, trafficGenPodName, networkFlowMonitorNamespace)
	return n.addon.Delete(ctx, n.EKSClient, n.Logger)
}
