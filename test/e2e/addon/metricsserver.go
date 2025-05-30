package addon

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/go-logr/logr"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned"

	ik8s "github.com/aws/eks-hybrid/internal/kubernetes"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

const (
	metricsServerNamespace = "kube-system"
	metricsServerName      = "metrics-server"
	metricsServerTimeout   = 2 * time.Minute
)

// MetricsServerTest tests the metrics-server addon
type MetricsServerTest struct {
	Cluster       string
	addon         *Addon
	K8S           clientgo.Interface
	EKSClient     *eks.Client
	K8SConfig     *rest.Config
	MetricsClient metricsv1beta1.Interface
	Logger        logr.Logger
}

// Create installs the metrics-server addon
func (m *MetricsServerTest) Create(ctx context.Context) error {
	m.addon = &Addon{
		Cluster:   m.Cluster,
		Namespace: metricsServerNamespace,
		Name:      metricsServerName,
	}

	if err := m.addon.CreateAndWaitForActive(ctx, m.EKSClient, m.K8S, m.Logger); err != nil {
		return fmt.Errorf("waiting for metrics-server addon: %v", err)
	}

	if err := kubernetes.DeploymentWaitForReplicas(ctx, metricsServerTimeout, m.K8S, metricsServerNamespace, metricsServerName); err != nil {
		return fmt.Errorf("waiting for metrics-server replicas: %v", err)
	}

	return nil
}

// Validate checks if metrics-server is providing metrics
func (m *MetricsServerTest) Validate(ctx context.Context) error {
	if err := validateNodeMetrics(ctx, m.MetricsClient, m.Logger); err != nil {
		return err
	}

	// pod metrics across all namespaces
	return validatePodMetrics(ctx, m.MetricsClient, m.Logger)
}

// PrintLogs collects and prints logs for debugging
func (m *MetricsServerTest) PrintLogs(ctx context.Context) error {
	logs, err := kubernetes.FetchLogs(ctx, m.K8S, m.addon.Name, m.addon.Namespace)
	if err != nil {
		return fmt.Errorf("failed to collect logs for %s: %v", m.addon.Name, err)
	}

	m.Logger.Info("Logs for metrics-server", "controller", logs)
	return nil
}

// Delete removes the addon
func (m *MetricsServerTest) Delete(ctx context.Context) error {
	return m.addon.Delete(ctx, m.EKSClient, m.Logger)
}

func validateNodeMetrics(ctx context.Context, metricsClient metricsv1beta1.Interface, logger logr.Logger) error {
	nodeMetrics, err := ik8s.ListRetry(ctx, metricsClient.MetricsV1beta1().NodeMetricses())
	if err != nil {
		return fmt.Errorf("listing node metrics: %v", err)
	}

	if len(nodeMetrics.Items) == 0 {
		return fmt.Errorf("no node metrics found")
	}

	for _, metrics := range nodeMetrics.Items {
		logger.Info("Found metrics for node", "node", metrics.Name,
			"cpu", metrics.Usage.Cpu().String(),
			"memory", metrics.Usage.Memory().String(),
		)
	}

	return nil
}

func validatePodMetrics(ctx context.Context, metricsClient metricsv1beta1.Interface, logger logr.Logger) error {
	podMetrics, err := ik8s.ListRetry(ctx, metricsClient.MetricsV1beta1().PodMetricses(""))
	if err != nil {
		return fmt.Errorf("listing pod metrics: %v", err)
	}

	if len(podMetrics.Items) == 0 {
		return fmt.Errorf("no pod metrics found")
	}

	logger.Info(fmt.Sprintf("Found metrics for %d pods", len(podMetrics.Items)))
	return nil
}
