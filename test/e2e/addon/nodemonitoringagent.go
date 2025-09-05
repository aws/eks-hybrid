package addon

import (
	"context"
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	ik8s "github.com/aws/eks-hybrid/internal/kubernetes"
	"github.com/aws/eks-hybrid/test/e2e/commands"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

const (
	nodeMonitoringAgentNamespace   = "kube-system"
	nodeMonitoringAgentName        = "eks-node-monitoring-agent"
	nodeMonitoringAgentWaitTimeout = 1 * time.Minute
)

type NodeMonitoringAgentTest struct {
	Cluster       string
	addon         *Addon
	K8S           clientgo.Interface
	EKSClient     *eks.Client
	K8SConfig     *rest.Config
	Logger        logr.Logger
	Command       string
	CommandRunner commands.RemoteCommandRunner
	NodeFilter    labels.Selector
}

func (n *NodeMonitoringAgentTest) Create(ctx context.Context) error {
	n.addon = &Addon{
		Cluster:   n.Cluster,
		Namespace: nodeMonitoringAgentNamespace,
		Name:      nodeMonitoringAgentName,
	}

	if err := n.addon.CreateAndWaitForActive(ctx, n.EKSClient, n.K8S, n.Logger); err != nil {
		return err
	}

	if err := kubernetes.DaemonSetWaitForReady(ctx, n.Logger, n.K8S, nodeMonitoringAgentNamespace, nodeMonitoringAgentName); err != nil {
		return err
	}

	return nil
}

func (n *NodeMonitoringAgentTest) runKernelError(ctx context.Context, nodes *v1.NodeList) error {
	for _, node := range nodes.Items {
		ip := kubernetes.GetNodeInternalIP(&node)
		if ip == "" {
			return fmt.Errorf("failed to get internal IP for node %s", node.Name)
		}
		// This command simulates a kernel error on the node
		// Node Monitoring Agent should detect this and create an event for SoftLockup
		// citing KernelReady as the reason.
		n.Logger.Info("Running kernel error simulation on node", "node", node.Name, "ip", ip)
		if _, err := n.CommandRunner.Run(ctx, ip, []string{n.Command}); err != nil {
			return fmt.Errorf("failed to run kernel error simulation on node %s: %w", node.Name, err)
		}
	}

	return nil
}

func (n *NodeMonitoringAgentTest) Validate(ctx context.Context) error {
	nodes, err := ik8s.ListRetry(ctx, n.K8S.CoreV1().Nodes(), func(opts *ik8s.ListOptions) {
		opts.LabelSelector = n.NodeFilter.String()
	})
	if err != nil {
		return err
	}

	if err := n.runKernelError(ctx, nodes); err != nil {
		return err
	}

	for _, node := range nodes.Items {
		nodeLogger := n.Logger.WithValues("node", node.Name)

		if err := validateNodeConditions(node, nodeLogger); err != nil {
			return err
		}

		if err := validateNodeEvents(ctx, n.K8S, node, nodeLogger); err != nil {
			return err
		}
	}

	return nil
}

func validateNodeConditions(node v1.Node, nodeLogger logr.Logger) error {
	foundMonitoringConditions := false
	for _, condition := range node.Status.Conditions {
		if isMonitoringAgentCondition(condition) {
			foundMonitoringConditions = true
			nodeLogger.Info("Found monitoring agent condition",
				"type", condition.Type,
				"status", condition.Status,
				"message", condition.Message)
			break
		}
	}

	if !foundMonitoringConditions {
		return fmt.Errorf("no monitoring agent conditions found")
	}

	return nil
}

func validateNodeEvents(ctx context.Context, k8s clientgo.Interface, node v1.Node, nodeLogger logr.Logger) error {
	fieldSelector := fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=Node", node.Name)

	_, err := ik8s.ListAndWait(ctx, nodeMonitoringAgentWaitTimeout, k8s.CoreV1().Events(""), func(events *v1.EventList) bool {
		foundMonitoringEvents := false
		for _, event := range events.Items {
			if isMonitoringAgentEvent(event) {
				foundMonitoringEvents = true
				nodeLogger.Info("Found monitoring agent event",
					"message", event.Message,
					"reason", event.Reason,
					"count", event.Count)
				break
			}
		}
		return foundMonitoringEvents
	}, func(opts *ik8s.ListOptions) { opts.FieldSelector = fieldSelector })
	if err != nil {
		return fmt.Errorf("failed to get kernel error event for node %s after retries: %v", node.Name, err)
	}

	return nil
}

// Helper function to identify monitoring agent conditions
func isMonitoringAgentCondition(condition v1.NodeCondition) bool {
	monitoringConditionTypes := []string{
		"StorageReady",
		"NetworkingReady",
		"KernelReady",
		"ContainerRuntimeReady",
	}

	return slices.Contains(monitoringConditionTypes, string(condition.Type))
}

func isMonitoringAgentEvent(event v1.Event) bool {
	return strings.Contains(strings.ToLower(event.Source.Component), nodeMonitoringAgentName)
}

func (n *NodeMonitoringAgentTest) PrintLogs(ctx context.Context) error {
	logs, err := kubernetes.FetchLogs(ctx, n.K8S, n.addon.Name, n.addon.Namespace, func(opts *ik8s.ListOptions) {
		opts.LabelSelector = fmt.Sprintf("%s=%s", "app.kubernetes.io/name", n.addon.Name)
	})
	if err != nil {
		return fmt.Errorf("failed to collect logs for %s: %v", n.addon.Name, err)
	}

	n.Logger.Info("Logs for node monitoring agent", "controller", logs)
	return nil
}

func (n *NodeMonitoringAgentTest) Delete(ctx context.Context) error {
	return n.addon.Delete(ctx, n.EKSClient, n.Logger)
}
