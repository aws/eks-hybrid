package kubernetes

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

// CreateResourceFromYAML creates a Kubernetes resource from YAML content
func CreateResourceFromYAML(ctx context.Context, logger logr.Logger, dynamicClient dynamic.Interface, yamlContent string) (*unstructured.Unstructured, error) {
	logger.Info("Creating resource from YAML")

	obj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal([]byte(yamlContent), &obj.Object); err != nil {
		return nil, fmt.Errorf("unmarshaling YAML: %w", err)
	}

	gvk := obj.GroupVersionKind()
	gvr := schema.GroupVersionResource{
		Group:    gvk.Group,
		Version:  gvk.Version,
		Resource: fmt.Sprintf("%ss", gvk.Kind),
	}

	if gvk.Kind == "AmazonCloudWatchAgent" {
		gvr.Resource = "amazoncloudwatchagents"
	}

	logger.Info("Creating resource", "name", obj.GetName(), "namespace", obj.GetNamespace(), "kind", gvk.Kind)

	created, err := dynamicClient.Resource(gvr).Namespace(obj.GetNamespace()).Create(
		ctx,
		obj,
		metav1.CreateOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("creating resource %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
	}

	logger.Info("Successfully created resource", "name", created.GetName(), "namespace", created.GetNamespace())
	return created, nil
}

// DeleteResource deletes a Kubernetes resource
func DeleteResource(ctx context.Context, logger logr.Logger, dynamicClient dynamic.Interface, gvr schema.GroupVersionResource, namespace, name string) error {
	logger.Info("Deleting resource", "name", name, "namespace", namespace, "gvr", gvr)

	err := dynamicClient.Resource(gvr).Namespace(namespace).Delete(
		ctx,
		name,
		metav1.DeleteOptions{},
	)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("deleting resource %s/%s: %w", namespace, name, err)
	}

	logger.Info("Successfully deleted resource", "name", name, "namespace", namespace)
	return nil
}
