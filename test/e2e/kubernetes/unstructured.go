package kubernetes

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apiyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/yaml"
)

// YamlToUnstructured takes a YAML and converts it to a list of Unstructured objects.
func YamlToUnstructured(rawyaml []byte) ([]unstructured.Unstructured, error) {
	var ret []unstructured.Unstructured

	reader := apiyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(rawyaml)))
	count := 1
	for {
		// Read one YAML document at a time, until io.EOF is returned
		b, err := reader.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("failed to read yaml: %w", err)
		}
		if len(b) == 0 {
			break
		}

		var m map[string]interface{}
		if err := yaml.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("failed to unmarshal the %d yaml document: %q: %w", count, string(b), err)
		}

		var u unstructured.Unstructured
		u.SetUnstructuredContent(m)

		// Ignore empty objects.
		// Empty objects are generated if there are weird things in manifest files like e.g. two --- in a row without a yaml doc in the middle
		if u.Object == nil {
			continue
		}

		ret = append(ret, u)
		count++
	}

	return ret, nil
}

// Creates, or Updates existing CR, foreach manifest
// Retries each up to 5 times
func UpsertManifestsWithRetries(ctx context.Context, k8s dynamic.Interface, manifests []unstructured.Unstructured) error {
	for _, obj := range manifests {
		err := retry.OnError(retry.DefaultRetry, func(err error) bool {
			// Retry any error type
			return true
		}, func() error {
			return upsertManifest(ctx, k8s, obj)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func upsertManifest(ctx context.Context, k8s dynamic.Interface, obj unstructured.Unstructured) error {
	groupVersion := obj.GroupVersionKind()
	resource, _ := meta.UnsafeGuessKindToResource(groupVersion)
	k8sResource := k8s.Resource(resource).Namespace(obj.GetNamespace())
	if _, err := k8sResource.Get(ctx, obj.GetName(), metav1.GetOptions{}); apierrors.IsNotFound(err) {
		fmt.Printf("Creating custom object %s (%s)\n", obj.GetName(), groupVersion)
		_, err := k8sResource.Create(ctx, &obj, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("creating custom object %s (%s): %w", obj.GetName(), groupVersion, err)
		}
	} else if err != nil {
		return fmt.Errorf("reading custom object %s (%s): %w", obj.GetName(), groupVersion, err)
	} else {
		fmt.Printf("Updating custom object %s (%s)\n", obj.GetName(), groupVersion)
		_, err := k8sResource.Update(ctx, &obj, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("updating custom object %s (%s): %w", obj.GetName(), groupVersion, err)
		}
	}

	return nil
}

func DeleteManifestsWithRetries(ctx context.Context, k8s dynamic.Interface, manifests []unstructured.Unstructured) error {
	for _, obj := range manifests {
		err := retry.OnError(retry.DefaultRetry, func(err error) bool {
			// Retry any error type
			return true
		}, func() error {
			return deleteManifest(ctx, k8s, obj)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func deleteManifest(ctx context.Context, k8s dynamic.Interface, obj unstructured.Unstructured) error {
	groupVersion := obj.GroupVersionKind()
	resource, _ := meta.UnsafeGuessKindToResource(groupVersion)
	k8sResource := k8s.Resource(resource).Namespace(obj.GetNamespace())
	if err := k8sResource.Delete(ctx, obj.GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting custom object %s (%s): %w", obj.GetName(), groupVersion, err)
	}

	return nil
}
