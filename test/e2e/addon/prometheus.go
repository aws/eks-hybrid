package addon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/go-logr/logr"
	clientgo "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	ik8s "github.com/aws/eks-hybrid/internal/kubernetes"
	"github.com/aws/eks-hybrid/internal/retry"
	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

const (
	prometheusName        = "prometheus-node-exporter"
	prometheusNamespace   = "prometheus-node-exporter"
	prometheusWaitTimeout = 2 * time.Minute
)

// PrometheusNodeExporterTest tests the Prometheus Node Exporter addon
type PrometheusNodeExporterTest struct {
	Cluster   string
	addon     *Addon
	K8S       clientgo.Interface
	EKSClient *eks.Client
	K8SConfig *rest.Config
	Logger    logr.Logger
}

// Create installs the node exporter addon
func (p *PrometheusNodeExporterTest) Create(ctx context.Context) error {
	p.addon = &Addon{
		Cluster:   p.Cluster,
		Namespace: prometheusNamespace,
		Name:      prometheusName,
	}

	if err := p.addon.CreateAndWaitForActive(ctx, p.EKSClient, p.K8S, p.Logger); err != nil {
		return err
	}

	if err := kubernetes.DaemonSetWaitForReady(ctx, p.Logger, p.K8S, prometheusNamespace, prometheusName); err != nil {
		return err
	}

	return nil
}

// Validate checks if Prometheus is scraping node exporter metrics
func (p *PrometheusNodeExporterTest) Validate(ctx context.Context) error {
	p.Logger.Info("Checking if Prometheus is scraping node exporter metrics")

	// Find the port that prometheus-node-exporter is using
	svc, err := ik8s.GetRetry(ctx, p.K8S.CoreV1().Services(prometheusNamespace), prometheusName)
	if err != nil {
		return fmt.Errorf("failed to get service: %v", err)
	}

	if len(svc.Spec.Ports) == 0 {
		return fmt.Errorf("no ports found for service")
	}

	port := svc.Spec.Ports[0].Port
	p.Logger.Info("Found service", "name", svc.Name, "port", port)

	metricsEndpoint := fmt.Sprintf("%s/api/v1/namespaces/%s/services/%s:%d/proxy/metrics",
		p.K8SConfig.Host, prometheusNamespace, prometheusName, port)

	// Check for node exporter metrics
	return p.checkNodeExporterMetrics(ctx, metricsEndpoint)
}

func (p *PrometheusNodeExporterTest) checkNodeExporterMetrics(ctx context.Context, metricsEndpoint string) error {
	p.Logger.Info("Checking for node exporter metrics", "endpoint", metricsEndpoint)

	roundTripper, err := rest.TransportFor(p.K8SConfig)
	if err != nil {
		return fmt.Errorf("failed to create HTTP transport: %v", err)
	}
	httpClient := &http.Client{Transport: roundTripper}

	ctx, cancel := context.WithTimeout(ctx, prometheusWaitTimeout)
	defer cancel()

	var metricsOutput string

	err = retry.NetworkRequest(ctx, func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, "GET", metricsEndpoint, nil)
		if err != nil {
			p.Logger.Error(err, "Failed to create HTTP request")
			return err
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			p.Logger.Error(err, "Failed to execute HTTP request")
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			p.Logger.Info("Non-OK status from metrics endpoint", "status", resp.StatusCode)
			return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			p.Logger.Error(err, "Failed to read response body")
			return err
		}

		metricsOutput = string(body)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to fetch metrics: %v", err)
	}

	// Check for expected metrics
	if !p.checkForExpectedMetrics(metricsOutput) {
		return fmt.Errorf("metrics validation failed: expected metrics not found")
	}

	return nil
}

func (p *PrometheusNodeExporterTest) checkForExpectedMetrics(metricsOutput string) bool {
	// Key node exporter metrics that should be present
	metricChecks := []string{
		"node_cpu_seconds_total",
		"node_memory_MemTotal_bytes",
		"node_filesystem_avail_bytes",
		"node_network_receive_bytes_total",
	}

	for _, metric := range metricChecks {
		if !strings.Contains(metricsOutput, metric) {
			p.Logger.Info("Missing expected metric", "metric", metric)
			return false
		}
		p.Logger.Info("Found expected metric", "metric", metric)
	}

	return true
}

// PrintLogs collects and prints logs for debugging
func (p *PrometheusNodeExporterTest) PrintLogs(ctx context.Context) error {
	logs, err := kubernetes.FetchLogs(ctx, p.K8S, p.addon.Name, p.addon.Namespace)
	if err != nil {
		return fmt.Errorf("failed to collect logs for %s: %v", p.addon.Name, err)
	}

	p.Logger.Info("Logs for prometheus-node-exporter", "controller", logs)
	return nil
}

// Delete removes the addon
func (p *PrometheusNodeExporterTest) Delete(ctx context.Context) error {
	return p.addon.Delete(ctx, p.EKSClient, p.Logger)
}
