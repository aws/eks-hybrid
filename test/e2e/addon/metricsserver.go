package addon

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

const (
	metricsServerNamespace = "kube-system"
	metricsServerName      = "metrics-server"
)

type MetricsServerTest struct {
	Cluster   string
	addon     *Addon
	K8S       clientgo.Interface
	EKSClient *eks.Client
	K8SConfig *rest.Config
	Logger    logr.Logger
}

func (m *MetricsServerTest) Run(ctx context.Context) error {
	m.addon = &Addon{
		Cluster:   m.Cluster,
		Namespace: metricsServerNamespace,
		Name:      metricsServerName,
	}

	if err := m.Create(ctx); err != nil {
		return err
	}

	if err := m.Validate(ctx); err != nil {
		return err
	}

	return nil
}

func (m *MetricsServerTest) Create(ctx context.Context) error {
	if err := m.addon.CreateAddon(ctx, m.EKSClient, m.K8S, m.Logger); err != nil {
		return err
	}

	if err := kubernetes.WaitForDeploymentReady(ctx, m.Logger, m.K8S, m.addon.Namespace, m.addon.Name); err != nil {
		return err
	}

	return nil
}

func (m *MetricsServerTest) Validate(ctx context.Context) error {
	metricsClient, err := metricsv1beta1.NewForConfig(m.K8SConfig)
	if err != nil {
		return fmt.Errorf("creating metrics client: %v", err)
	}

	if err := getNodeMetrics(ctx, metricsClient, m.Logger); err != nil {
		return err
	}

	// pod metrics across all namespaces
	return getPodMetrics(ctx, metricsClient, m.Logger)
}

func (m *MetricsServerTest) CollectLogs(ctx context.Context) error {
	return m.addon.FetchLogs(ctx, m.K8S, m.Logger, []string{metricsServerName}, tailLines)
}

func getNodeMetrics(ctx context.Context, metricsClient *metricsv1beta1.Clientset, logger logr.Logger) error {
	consecutiveErrors := 0

	// Add initial check for metrics-server availability
	_, err := metricsClient.Discovery().ServerVersion()
	if err != nil {
		logger.Error(err, "Failed to connect to metrics server")
		return err
	}

	err = wait.ExponentialBackoffWithContext(ctx, retryBackoff, func(ctx context.Context) (bool, error) {
		allNodeMetrics, err := metricsClient.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
		if err != nil {
			if errors.IsServiceUnavailable(err) {
				logger.Info("Metrics server is temporarily unavailable", "error", err)
				consecutiveErrors++
				if consecutiveErrors > addonMaxRetries {
					return false, fmt.Errorf("metrics server repeatedly unavailable: %v", err)
				}
				return false, nil
			}

			return false, fmt.Errorf("unexpected error getting metrics: %v", err)
		}
		for _, metrics := range allNodeMetrics.Items {
			logger.Info("Found metrics for node", "node", metrics.Name,
				"cpu", metrics.Usage.Cpu().String(),
				"memory", metrics.Usage.Memory().String(),
			)
		}

		return true, nil
	})

	return err
}

func getPodMetrics(ctx context.Context, metricsClient *metricsv1beta1.Clientset, logger logr.Logger) error {
	consecutiveErrors := 0

	err := wait.ExponentialBackoffWithContext(ctx, retryBackoff, func(ctx context.Context) (bool, error) {
		pods, err := metricsClient.MetricsV1beta1().PodMetricses("").List(ctx, metav1.ListOptions{})
		if err != nil {
			consecutiveErrors += 1
			if consecutiveErrors > addonMaxRetries {
				return false, fmt.Errorf("getting pod metrics: %v", err)
			}
			logger.Info("Retryable error getting pod metrics. Continuing to poll", "error", err)
			return false, nil // continue polling
		}

		logger.Info(fmt.Sprintf("Found metrics for %d pods", len(pods.Items)))
		return true, nil
	})

	return err
}

func (m *MetricsServerTest) Delete(ctx context.Context) error {
	return m.addon.Delete(ctx, m.EKSClient, m.Logger)
}
