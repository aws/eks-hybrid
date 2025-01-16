package cni

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"

	"github.com/aws/eks-hybrid/test/e2e/constants"
)

//go:embed testdata/cilium/cilium-template.yaml
var ciliumTemplate []byte

type Cilium struct {
	k8s dynamic.Interface
	// podCIDR is the cluster level CIDR to be use for Pods. It needs to be big enough for
	// Hybrid Nodes.
	//
	// Check the cilium-template file for the node pod cidr mask. The default is 24.
	podCIDR string
	region  string
}

func NewCilium(k8s dynamic.Interface, podCIDR, region string) Cilium {
	return Cilium{
		k8s:     k8s,
		podCIDR: podCIDR,
		region:  region,
	}
}

// Deploy creates or updates the Cilium reosurces.
func (c Cilium) Deploy(ctx context.Context) error {
	tmpl, err := template.New("cilium").Parse(string(ciliumTemplate))
	if err != nil {
		return err
	}
	values := map[string]string{
		"PodCIDR":           c.podCIDR,
		"ContainerRegistry": constants.EcrAccounId + ".dkr.ecr." + c.region + ".amazonaws.com/quay.io",
	}
	installation := &bytes.Buffer{}
	err = tmpl.Execute(installation, values)
	if err != nil {
		return err
	}

	objs, err := yamlToUnstructured(installation.Bytes())
	if err != nil {
		return err
	}

	fmt.Println("Applying cilium installation")

	return upsertManifestsWithRetries(ctx, c.k8s, objs)
}

// Creates, or Updates existing CR, foreach manifest
// Retries each up to 5 times
func upsertManifestsWithRetries(ctx context.Context, k8s dynamic.Interface, manifests []unstructured.Unstructured) error {
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
	resource := schema.GroupVersionResource{
		Group:    groupVersion.Group,
		Version:  groupVersion.Version,
		Resource: strings.ToLower(groupVersion.Kind + "s"),
	}
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
