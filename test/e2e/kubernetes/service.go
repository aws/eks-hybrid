package kubernetes

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"

	ik8s "github.com/aws/eks-hybrid/internal/kubernetes"
)

// CreateService creates a service with the specified configuration
func CreateService(
	ctx context.Context,
	k8s kubernetes.Interface,
	name, namespace string,
	selector map[string]string,
	servicePort, targetPort int32,
	logger logr.Logger,
	additionalLabels ...map[string]string,
) (*corev1.Service, error) {
	// Start with default labels
	labels := map[string]string{
		"app": name,
	}

	// Add additional labels if provided
	if len(additionalLabels) > 0 {
		for _, labelMap := range additionalLabels {
			for key, value := range labelMap {
				labels[key] = value
			}
		}
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports: []corev1.ServicePort{
				{
					Port:       servicePort,
					TargetPort: intstr.FromInt(int(targetPort)),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	createdService, err := k8s.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("creating service %s: %w", name, err)
	}

	logger.Info("Created service", "name", name, "namespace", namespace)
	return createdService, nil
}

// WaitForServiceReady waits for service endpoints to be ready
func WaitForServiceReady(ctx context.Context, k8s kubernetes.Interface, serviceName, namespace string, logger logr.Logger) error {
	logger.Info("Waiting for service endpoints", "service", serviceName)

	_, err := ik8s.ListAndWait(ctx, 3*time.Minute, k8s.DiscoveryV1().EndpointSlices(namespace), func(endpointSlices *discoveryv1.EndpointSliceList) bool {
		var totalEndpoints int
		var readyEndpoints int

		for _, slice := range endpointSlices.Items {
			if ownerService, exists := slice.Labels["kubernetes.io/service-name"]; exists && ownerService == serviceName {
				for _, endpoint := range slice.Endpoints {
					totalEndpoints++
					if endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready {
						readyEndpoints++
					}
				}
			}
		}

		// Service is ready when all endpoints are ready
		if totalEndpoints > 0 && readyEndpoints == totalEndpoints {
			logger.Info("All service endpoints are ready", "service", serviceName, "ready", readyEndpoints, "total", totalEndpoints)
			return true
		}

		if totalEndpoints > 0 {
			logger.Info("Waiting for all service endpoints to be ready", "service", serviceName, "ready", readyEndpoints, "total", totalEndpoints)
		}

		return false
	})
	if err != nil {
		return fmt.Errorf("service %s endpoints never became ready: %w", serviceName, err)
	}

	logger.Info("Service has all endpoints ready", "service", serviceName)
	return nil
}

// TestServiceConnectivityWithRetries tests service connectivity using short name with 5-minute retry window
func TestServiceConnectivityWithRetries(ctx context.Context, config *restclient.Config, k8s kubernetes.Interface, clientPodName, serviceName, namespace string, port int32, logger logr.Logger) error {
	logger.Info("Testing service connectivity with retries", "from", clientPodName, "service", serviceName, "port", port)

	serviceURL := fmt.Sprintf("http://%s:%d", serviceName, port)

	cmd := []string{
		"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}",
		serviceURL,
		"--connect-timeout", "60",
		"--max-time", "120",
		"--retry", "5",
		"--retry-delay", "30",
		"--retry-max-time", "600",
		"--retry-all-errors",
	}

	_, err := ik8s.GetAndWait(ctx, 15*time.Minute, k8s.CoreV1().Pods(namespace), clientPodName, func(pod *corev1.Pod) bool {
		if pod.Status.Phase != corev1.PodRunning {
			return false
		}

		result, _, execErr := execPod(ctx, config, k8s, clientPodName, namespace, cmd...)
		if execErr != nil {
			logger.Info("Service connectivity test failed", "error", execErr.Error())
			return false
		}

		return strings.Contains(result, "200")
	})
	if err != nil {
		return fmt.Errorf("service connectivity test failed from %s to %s: %w", clientPodName, serviceName, err)
	}

	logger.Info("Service connectivity test completed successfully", "from", clientPodName, "to", serviceName)
	return nil
}

// DeleteServiceAndWait deletes a service and waits for it to be fully removed
func DeleteServiceAndWait(ctx context.Context, k8s kubernetes.Interface, serviceName, namespace string, logger logr.Logger) error {
	logger.Info("Deleting service and waiting for removal", "service", serviceName)

	err := ik8s.IdempotentDelete(ctx, k8s.CoreV1().Services(namespace), serviceName)
	if err != nil {
		return fmt.Errorf("deleting service %s in namespace %s: %w", serviceName, namespace, err)
	}

	_, err = ik8s.ListAndWait(ctx, 30*time.Second, k8s.CoreV1().Services(namespace), func(services *corev1.ServiceList) bool {
		for _, service := range services.Items {
			if service.Name == serviceName {
				return false
			}
		}
		return true
	}, func(opts *ik8s.ListOptions) {
		opts.FieldSelector = "metadata.name=" + serviceName
	})
	if err != nil {
		return fmt.Errorf("waiting for service %s to be deleted: %w", serviceName, err)
	}

	logger.Info("Service fully deleted", "service", serviceName)
	return nil
}

// ListServicesWithLabels lists services in a namespace that match the given label selector
func ListServicesWithLabels(ctx context.Context, k8s kubernetes.Interface, namespace, labelSelector string) (*corev1.ServiceList, error) {
	services, err := k8s.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list services with selector %s: %w", labelSelector, err)
	}
	return services, nil
}

// DeleteServicesWithLabels deletes all services in a namespace that match the given label selector
func DeleteServicesWithLabels(ctx context.Context, k8s kubernetes.Interface, namespace, labelSelector string, logger logr.Logger) error {
	logger.Info("Deleting services with label selector", "selector", labelSelector, "namespace", namespace)

	services, err := ListServicesWithLabels(ctx, k8s, namespace, labelSelector)
	if err != nil {
		return fmt.Errorf("failed to list services with selector %s: %w", labelSelector, err)
	}

	for _, service := range services.Items {
		if err := DeleteServiceAndWait(ctx, k8s, service.Name, namespace, logger); err != nil {
			logger.Info("Service cleanup: resource not found or already deleted", "name", service.Name)
		} else {
			logger.Info("Deleted service", "name", service.Name)
		}
	}
	return nil
}

// ConfigureKubeDNSTrafficDistribution configures kube-dns service for traffic distribution
func ConfigureKubeDNSTrafficDistribution(ctx context.Context, k8s kubernetes.Interface, logger logr.Logger) error {
	serviceTrafficPatch := `{
		"spec": {
			"trafficDistribution": "PreferClose"
		}
	}`

	_, err := k8s.CoreV1().Services("kube-system").Patch(ctx, "kube-dns",
		"application/merge-patch+json", []byte(serviceTrafficPatch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to configure kube-dns traffic distribution: %w", err)
	}

	logger.Info("Configured kube-dns service with PreferClose traffic distribution")
	return nil
}
