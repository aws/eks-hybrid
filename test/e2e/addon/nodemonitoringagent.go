package addon

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"

	"slices"

	"github.com/aws/eks-hybrid/test/e2e/errors"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

const (
	nodeMonitoringAgentNamespace = "kube-system"
	nodeMonitoringAgentName      = "eks-node-monitoring-agent"
	lockupDaemonSetName          = "kernel-soft-lockup"
	lockupContainerNamespace     = "default"
)

type NodeMonitoringAgentTest struct {
	Cluster   string
	addon     *Addon
	K8S       clientgo.Interface
	EKSClient *eks.Client
	K8SConfig *rest.Config
	Logger    logr.Logger
}

func (n *NodeMonitoringAgentTest) Run(ctx context.Context) error {
	n.addon = &Addon{
		Cluster:   n.Cluster,
		Namespace: nodeMonitoringAgentNamespace,
		Name:      nodeMonitoringAgentName,
	}

	if err := n.Create(ctx); err != nil {
		return err
	}

	if err := n.Validate(ctx); err != nil {
		return err
	}

	return nil
}

func (n NodeMonitoringAgentTest) Create(ctx context.Context) error {
	if err := n.addon.CreateAddon(ctx, n.EKSClient, n.K8S, n.Logger); err != nil {
		return err
	}

	if err := kubernetes.WaitForDaemonSetReady(ctx, n.Logger, n.K8S, n.addon.Namespace, n.addon.Name); err != nil {
		return err
	}

	if err := n.deployKernelError(ctx); err != nil {
		return err
	}

	return nil
}

func (n NodeMonitoringAgentTest) deployKernelError(ctx context.Context) error {
	labels := map[string]string{"app": lockupDaemonSetName}
	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      lockupDaemonSetName,
			Namespace: lockupContainerNamespace,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  lockupDaemonSetName,
							Image: "public.ecr.aws/amazonlinux/amazonlinux:2023-minimal",
							SecurityContext: &v1.SecurityContext{
								Privileged: ptr.To(true),
								RunAsUser:  ptr.To(int64(0)),
							},
							Command: []string{
								"/bin/bash",
								"-c",
								"echo 'watchdog: BUG: soft lockup - CPU#6 stuck for 23s! [VM Thread:4054]' | tee -a /host/dev/kmsg",
							},
							VolumeMounts: []v1.VolumeMount{
								{
									Name:      "host-root",
									MountPath: "/host",
								},
							},
						},
					},
					Volumes: []v1.Volume{
						{
							Name: "host-root",
							VolumeSource: v1.VolumeSource{
								HostPath: &v1.HostPathVolumeSource{
									Path: "/",
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := n.K8S.AppsV1().DaemonSets(lockupContainerNamespace).Create(
		ctx,
		daemonSet,
		metav1.CreateOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to create kernel lockup daemonset: %w", err)
	}

	if err := kubernetes.WaitForDaemonSetReady(ctx, n.Logger, n.K8S, daemonSet.Namespace, daemonSet.Name); err != nil {
		return err
	}

	return nil
}

func (n NodeMonitoringAgentTest) Validate(ctx context.Context) error {
	var nodes *v1.NodeList

	err := wait.ExponentialBackoffWithContext(ctx, retryBackoff, func(ctx context.Context) (bool, error) {
		var err error
		nodes, err = n.K8S.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			n.Logger.Error(err, "Failed to list nodes")
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to list nodes after retries: %v", err)
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

	var events *v1.EventList
	err := wait.ExponentialBackoffWithContext(ctx, retryBackoff, func(ctx context.Context) (bool, error) {
		var err error
		events, err = k8s.CoreV1().Events("").List(ctx, metav1.ListOptions{
			FieldSelector: fieldSelector,
		})
		if err != nil {
			nodeLogger.Error(err, "Failed to get events")
			return false, nil
		}

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

		if !foundMonitoringEvents {
			return false, nil
		}

		return true, nil
	})
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

func (n NodeMonitoringAgentTest) CollectLogs(ctx context.Context) error {
	logs, err := kubernetes.FetchLogs(ctx, n.K8S, n.addon.Name, n.addon.Namespace)
	if err != nil {
		return fmt.Errorf("failed to collect logs for %s: %v", n.addon.Name, err)
	}

	n.Logger.Info("Logs for node monitoring agent", "controller", logs)
	return nil
}

func (n NodeMonitoringAgentTest) Delete(ctx context.Context) error {
	// Delete kernel lockup daemonset
	if err := n.K8S.AppsV1().DaemonSets(lockupContainerNamespace).Delete(ctx, lockupDaemonSetName, metav1.DeleteOptions{}); err != nil && !errors.IsType(err, &types.ResourceNotFoundException{}) {
		return err
	}

	return n.addon.Delete(ctx, n.EKSClient, n.Logger)
}
