package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

// PatchCRDWithMerge patches a Custom Resource using strategic merge patch
func PatchCRDWithMerge(ctx context.Context, logger logr.Logger, dynamicClient dynamic.Interface, gvr schema.GroupVersionResource, namespace, name string, patchData map[string]interface{}) error {
	logger.Info("Patching CRD with merge patch", "name", name, "namespace", namespace, "gvr", gvr)

	patchBytes, err := json.Marshal(patchData)
	if err != nil {
		return fmt.Errorf("marshaling patch data: %w", err)
	}

	_, err = dynamicClient.Resource(gvr).Namespace(namespace).Patch(
		ctx,
		name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("patching CRD %s/%s: %w", namespace, name, err)
	}

	logger.Info("Successfully patched CRD", "name", name, "namespace", namespace)
	return nil
}

// CreateCRDFromYAML creates a Custom Resource from YAML content
func CreateCRDFromYAML(ctx context.Context, logger logr.Logger, dynamicClient dynamic.Interface, yamlContent string) (*unstructured.Unstructured, error) {
	logger.Info("Creating CRD from YAML")

	// Convert YAML to unstructured object
	obj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal([]byte(yamlContent), &obj.Object); err != nil {
		return nil, fmt.Errorf("unmarshaling YAML: %w", err)
	}

	// Get GVR from the object
	gvk := obj.GroupVersionKind()
	gvr := schema.GroupVersionResource{
		Group:    gvk.Group,
		Version:  gvk.Version,
		Resource: fmt.Sprintf("%ss", gvk.Kind),
	}

	if gvk.Kind == "AmazonCloudWatchAgent" {
		gvr.Resource = "amazoncloudwatchagents"
	}

	logger.Info("Creating CRD", "name", obj.GetName(), "namespace", obj.GetNamespace(), "kind", gvk.Kind)

	// Create the resource
	created, err := dynamicClient.Resource(gvr).Namespace(obj.GetNamespace()).Create(
		ctx,
		obj,
		metav1.CreateOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("creating CRD %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
	}

	logger.Info("Successfully created CRD", "name", created.GetName(), "namespace", created.GetNamespace())
	return created, nil
}

// DeleteCRD deletes a Custom Resource
func DeleteCRD(ctx context.Context, logger logr.Logger, dynamicClient dynamic.Interface, gvr schema.GroupVersionResource, namespace, name string) error {
	logger.Info("Deleting CRD", "name", name, "namespace", namespace, "gvr", gvr)

	err := dynamicClient.Resource(gvr).Namespace(namespace).Delete(
		ctx,
		name,
		metav1.DeleteOptions{},
	)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("deleting CRD %s/%s: %w", namespace, name, err)
	}

	logger.Info("Successfully deleted CRD", "name", name, "namespace", namespace)
	return nil
}

// PatchCloudWatchAgentCRD patches the AmazonCloudWatchAgent CRD with IRSA environment variables
func PatchCloudWatchAgentCRD(ctx context.Context, dynamicClient dynamic.Interface, logger logr.Logger, namespace string) error {
	logger.Info("Patching AmazonCloudWatchAgent CRD")

	// Patch data structure for adding environment variables to the CRD
	patchData := map[string]interface{}{
		"spec": map[string]interface{}{
			"env": []map[string]interface{}{
				{
					"name":  "RUN_WITH_IRSA",
					"value": "True",
				},
				{
					"name": "HOST_IP",
					"valueFrom": map[string]interface{}{
						"fieldRef": map[string]interface{}{
							"fieldPath": "status.hostIP",
						},
					},
				},
				{
					"name": "HOST_NAME",
					"valueFrom": map[string]interface{}{
						"fieldRef": map[string]interface{}{
							"fieldPath": "spec.nodeName",
						},
					},
				},
				{
					"name": "K8S_NAMESPACE",
					"valueFrom": map[string]interface{}{
						"fieldRef": map[string]interface{}{
							"fieldPath": "metadata.namespace",
						},
					},
				},
			},
		},
	}

	gvr := schema.GroupVersionResource{
		Group:    "cloudwatch.aws.amazon.com",
		Version:  "v1alpha1",
		Resource: "amazoncloudwatchagents",
	}
	if err := PatchCRDWithMerge(ctx, logger, dynamicClient, gvr, namespace, "cloudwatch-agent", patchData); err != nil {
		return fmt.Errorf("patching AmazonCloudWatchAgent CRD: %w", err)
	}

	logger.Info("CloudWatch mutating webhook is working - successfully processed and allowed valid CRD patch operation")
	return nil
}
