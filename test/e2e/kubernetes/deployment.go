package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	ik8s "github.com/aws/eks-hybrid/internal/kubernetes"
)

// CreateDeployment creates a deployment with the specified configuration
func CreateDeployment(
	ctx context.Context,
	k8s kubernetes.Interface,
	name, namespace, region string,
	nodeSelector map[string]string,
	targetPort int32,
	replicas int32,
	logger logr.Logger,
	dnsSuffix, ecrAccount string,
	additionalLabels ...map[string]string,
) (*appsv1.Deployment, error) {
	actualTargetPort := int32(80) // nginx deployments use port 80
	containerCommand := []string{"nginx", "-g", "daemon off;"}

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

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
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
							Image:   fmt.Sprintf("%s.dkr.ecr.%s.%s/ecr-public/nginx/nginx:latest", ecrAccount, region, dnsSuffix),
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

	createdDeployment, err := k8s.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("creating deployment %s: %w", name, err)
	}

	logger.Info("Created deployment", "name", name, "namespace", namespace, "replicas", replicas)
	return createdDeployment, nil
}

func DeploymentWaitForReplicas(ctx context.Context, timeout time.Duration, k8s kubernetes.Interface, namespace, name string) error {
	if _, err := ik8s.GetAndWait(ctx, timeout, k8s.AppsV1().Deployments(namespace), name, func(d *appsv1.Deployment) bool {
		return d.Status.Replicas == d.Status.ReadyReplicas
	}); err != nil {
		return fmt.Errorf("deployment %s replicas never became ready: %v", name, err)
	}

	return nil
}

// DeleteDeploymentAndWait deletes a deployment and waits for it to be fully removed
func DeleteDeploymentAndWait(ctx context.Context, k8s kubernetes.Interface, deploymentName, namespace string, logger logr.Logger) error {
	logger.Info("Deleting deployment and waiting for removal", "deployment", deploymentName)

	err := ik8s.IdempotentDelete(ctx, k8s.AppsV1().Deployments(namespace), deploymentName)
	if err != nil {
		return fmt.Errorf("deleting deployment %s in namespace %s: %w", deploymentName, namespace, err)
	}

	// Wait for deployment to be fully deleted
	_, err = ik8s.ListAndWait(ctx, 60*time.Second, k8s.AppsV1().Deployments(namespace), func(deployments *appsv1.DeploymentList) bool {
		for _, deployment := range deployments.Items {
			if deployment.Name == deploymentName {
				return false
			}
		}
		return true
	}, func(opts *ik8s.ListOptions) {
		opts.FieldSelector = "metadata.name=" + deploymentName
	})
	if err != nil {
		return fmt.Errorf("waiting for deployment %s to be deleted: %w", deploymentName, err)
	}

	logger.Info("Deployment fully deleted", "deployment", deploymentName)
	return nil
}

// ListDeploymentsWithLabels lists deployments in a namespace that match the given label selector
func ListDeploymentsWithLabels(ctx context.Context, k8s kubernetes.Interface, namespace, labelSelector string) (*appsv1.DeploymentList, error) {
	deployments, err := k8s.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments with selector %s: %w", labelSelector, err)
	}
	return deployments, nil
}

// DeleteDeploymentsWithLabels deletes all deployments in a namespace that match the given label selector
func DeleteDeploymentsWithLabels(ctx context.Context, k8s kubernetes.Interface, namespace, labelSelector string, logger logr.Logger) error {
	logger.Info("Deleting deployments with label selector", "selector", labelSelector, "namespace", namespace)

	deployments, err := ListDeploymentsWithLabels(ctx, k8s, namespace, labelSelector)
	if err != nil {
		return fmt.Errorf("failed to list deployments with selector %s: %w", labelSelector, err)
	}

	for _, deployment := range deployments.Items {
		if err := DeleteDeploymentAndWait(ctx, k8s, deployment.Name, namespace, logger); err != nil {
			logger.Info("Deployment cleanup: resource not found or already deleted", "name", deployment.Name)
		} else {
			logger.Info("Deleted deployment", "name", deployment.Name)
		}
	}
	return nil
}

// ConfigureCoreDNSAntiAffinity configures CoreDNS deployment with preferred anti-affinity rules
func ConfigureCoreDNSAntiAffinity(ctx context.Context, k8s kubernetes.Interface, logger logr.Logger) error {
	coreDNSPatch := `{
		"spec": {
			"replicas": 2,
			"template": {
				"spec": {
					"affinity": {
						"podAntiAffinity": {
							"preferredDuringSchedulingIgnoredDuringExecution": [
								{
									"weight": 100,
									"podAffinityTerm": {
										"labelSelector": {
											"matchExpressions": [
												{
													"key": "k8s-app",
													"operator": "In",
													"values": ["kube-dns"]
												}
											]
										},
										"topologyKey": "kubernetes.io/hostname"
									}
								},
								{
									"weight": 50,
									"podAffinityTerm": {
										"labelSelector": {
											"matchExpressions": [
												{
													"key": "k8s-app",
													"operator": "In", 
													"values": ["kube-dns"]
												}
											]
										},
										"topologyKey": "topology.kubernetes.io/zone"
									}
								}
							]
						}
					}
				}
			}
		}
	}`

	_, err := k8s.AppsV1().Deployments("kube-system").Patch(ctx, "coredns",
		"application/merge-patch+json", []byte(coreDNSPatch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to configure CoreDNS preferred anti-affinity: %w", err)
	}

	logger.Info("Configured CoreDNS deployment with preferred anti-affinity rules")
	return nil
}

// RemoveDeploymentAntiAffinity removes node affinity rules from a deployment that would prevent pods from being scheduled on hybrid nodes.
// This is useful to test EKS add-on before anti-affinity rule for hybrid nodes is removed.
// Once anti-affinity rule is removed, then caller no longer needs to call this method.
func RemoveDeploymentAntiAffinity(ctx context.Context, k8s kubernetes.Interface, name, namespace string, logger logr.Logger) error {
	logger.Info("Removing node affinity rules to allow scheduling on hybrid nodes", "deployment", name, "namespace", namespace)

	// Get the current deployment to check if it has affinity rules
	deployment, err := k8s.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting deployment %s in namespace %s: %w", name, namespace, err)
	}

	if deployment.Spec.Template.Spec.Affinity == nil {
		logger.Info("Deployment has no affinity rules, nothing to remove", "deployment", name)
		return nil
	}

	if deployment.Spec.Template.Spec.Affinity.NodeAffinity == nil {
		logger.Info("Deployment has no node affinity rules", "deployment", name)
		return nil
	}

	// Create a JSON patch to remove the nodeAffinity field
	// For now we remove all nodeAffinity rules, which should be okay for our e2e tests
	patchJSON := []map[string]interface{}{
		{
			"op":   "remove",
			"path": "/spec/template/spec/affinity/nodeAffinity",
		},
	}

	patchBytes, err := json.Marshal(patchJSON)
	if err != nil {
		return fmt.Errorf("marshaling deployment patch: %w", err)
	}

	// Apply the JSON patch
	_, err = k8s.AppsV1().Deployments(namespace).Patch(ctx, name, types.JSONPatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("patching deployment %s to remove node affinity rules: %w", name, err)
	}

	logger.Info("Successfully removed node affinity rules from deployment", "deployment", name)
	return nil
}
