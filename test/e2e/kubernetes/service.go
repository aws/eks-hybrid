package kubernetes

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"

	ik8s "github.com/aws/eks-hybrid/internal/kubernetes"
)

// CreateServiceWithDeployment creates a service and deployment with the specified configuration
func CreateServiceWithDeployment(
	ctx context.Context,
	k8s kubernetes.Interface,
	name, image string,
	nodeSelector map[string]string,
	servicePort, targetPort int32,
	replicas int32,
	logger logr.Logger,
) (*corev1.Service, *appsv1.Deployment, error) {
	actualTargetPort := targetPort
	var containerCommand []string
	if strings.Contains(image, "nginx") {
		actualTargetPort = 8080
		containerCommand = []string{"sh", "-c", "sed -i 's/listen.*80;/listen 8080;/' /etc/nginx/conf.d/default.conf && nginx -g 'daemon off;'"}
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{"app": name},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": name},
				},
				Spec: corev1.PodSpec{
					NodeSelector: nodeSelector,
					Containers: []corev1.Container{
						{
							Name:    name,
							Image:   image,
							Command: containerCommand,
							Ports: []corev1.ContainerPort{
								{ContainerPort: actualTargetPort, Protocol: corev1.ProtocolTCP},
							},
						},
					},
				},
			},
		},
	}

	createdDeployment, err := k8s.AppsV1().Deployments("default").Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("creating deployment %s: %w", name, err)
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{"app": name},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": name},
			Ports: []corev1.ServicePort{
				{
					Port:       8080,
					TargetPort: intstr.FromInt(int(actualTargetPort)),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	createdService, err := k8s.CoreV1().Services("default").Create(ctx, service, metav1.CreateOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("creating service %s: %w", name, err)
	}

	logger.Info("Created service with deployment", "name", name, "replicas", replicas)
	return createdService, createdDeployment, nil
}

// WaitForServiceReady waits for service endpoints to be ready
func WaitForServiceReady(ctx context.Context, k8s kubernetes.Interface, serviceName, namespace string, logger logr.Logger) error {
	logger.Info("Waiting for service endpoints", "service", serviceName)

	_, err := ik8s.ListAndWait(ctx, 3*time.Minute, k8s.DiscoveryV1().EndpointSlices(namespace), func(endpointSlices *discoveryv1.EndpointSliceList) bool {
		for _, slice := range endpointSlices.Items {
			if ownerService, exists := slice.Labels["kubernetes.io/service-name"]; exists && ownerService == serviceName {
				for _, endpoint := range slice.Endpoints {
					if endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready {
						return true
					}
				}
			}
		}
		return false
	})
	if err != nil {
		return fmt.Errorf("service %s endpoints never became ready: %w", serviceName, err)
	}

	logger.Info("Service has endpoints", "service", serviceName)
	return nil
}

// TestServiceConnectivityWithRetries tests service connectivity using short name with 5-minute retry window
func TestServiceConnectivityWithRetries(ctx context.Context, config *restclient.Config, k8s kubernetes.Interface, clientPodName, serviceName, namespace string, port int32, logger logr.Logger) error {
	logger.Info("Testing service connectivity with retries", "from", clientPodName, "service", serviceName, "port", port)

	serviceURL := fmt.Sprintf("http://%s:%d", serviceName, port)
	cmd := []string{"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", serviceURL, "--connect-timeout", "30", "--max-time", "60"}

	_, err := ik8s.GetAndWait(ctx, 5*time.Minute, k8s.CoreV1().Pods(namespace), clientPodName, func(pod *corev1.Pod) bool {
		if pod.Status.Phase != corev1.PodRunning {
			return false
		}

		result, execErr := ExecInPod(ctx, config, k8s, clientPodName, namespace, cmd)
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
