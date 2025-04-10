package cni

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"

	"github.com/aws/eks-hybrid/test/e2e/kubernetes"
)

// NodePodCIDRs returns the pod CIDRs assigned to a node by the CNI.
// Only Cilium and Calico are supported.
func NodePodCIDRs(ctx context.Context, k8s dynamic.Interface, node *corev1.Node) ([]string, error) {
	networkCondition := kubernetes.NetworkUnavailableCondition(node)
	if networkCondition == nil {
		return nil, fmt.Errorf("node %s does not have network unavailable condition, can't determine CNI", node.Name)
	}

	if networkCondition.Status != corev1.ConditionFalse {
		return nil, fmt.Errorf("node %s network is unavailable because %s: %s", node.Name, networkCondition.Status, networkCondition.Message)
	}

	switch networkCondition.Reason {
	case "CiliumIsUp":
		return ciliumNodePodCIDR(ctx, k8s, node)
	case "CalicoIsUp":
		return calicoNodePodCIDR(ctx, k8s, node)
	default:
		return nil, fmt.Errorf("unsupported CNI, network condition with reason [%s]: %s", networkCondition.Reason, networkCondition.Message)
	}
}

func ciliumNodePodCIDR(ctx context.Context, k8s dynamic.Interface, node *corev1.Node) ([]string, error) {
	ciliumNodeGVR := schema.GroupVersionResource{
		Group:    "cilium.io",
		Version:  "v2",
		Resource: "ciliumnodes",
	}

	obj, err := kubernetes.RetryGet(ctx, k8s.Resource(ciliumNodeGVR), node.Name)
	if err != nil {
		return nil, err
	}
	podCIDRs, found, err := unstructured.NestedStringSlice(obj.Object, "spec", "ipam", "podCIDRs")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("podCIDRs not found in CiliumNode %s", node.Name)
	}
	return podCIDRs, nil
}

func calicoNodePodCIDR(ctx context.Context, k8s dynamic.Interface, node *corev1.Node) ([]string, error) {
	var podCIDRs []string
	err := retry.OnError(retry.DefaultBackoff, func(err error) bool {
		// retry on any error
		return true
	}, func() error {
		var err error
		podCIDRs, err = getCalicoNodePodCIDR(ctx, k8s, node)
		return err
	})

	return podCIDRs, err
}

func getCalicoNodePodCIDR(ctx context.Context, k8s dynamic.Interface, node *corev1.Node) ([]string, error) {
	ipamBlockGVR := schema.GroupVersionResource{
		Group:    "crd.projectcalico.org",
		Version:  "v1",
		Resource: "ipamblocks",
	}

	ipamBlocks, err := kubernetes.RetryList(ctx, k8s.Resource(ipamBlockGVR))
	if err != nil {
		return nil, err
	}
	var podCIDRs []string
	for _, b := range ipamBlocks.Items {
		block := &calicoIPAMBlock{obj: &b}

		nodeName, err := block.nodeName()
		if err != nil {
			return nil, err
		}
		if nodeName != node.Name {
			continue
		}

		blockPodCIDRs, err := block.podCIDRs()
		if err != nil {
			return nil, err
		}
		podCIDRs = append(podCIDRs, blockPodCIDRs...)
	}

	if len(podCIDRs) == 0 {
		return nil, fmt.Errorf("no calico IPAMBlock found for node %s", node.Name)
	}

	return podCIDRs, nil
}

type calicoIPAMBlock struct {
	obj *unstructured.Unstructured
}

func (i *calicoIPAMBlock) nodeName() (string, error) {
	affinity, found, err := unstructured.NestedString(i.obj.Object, "spec", "affinity")
	if err != nil {
		return "", fmt.Errorf("reading affinity from Calico IPAMBlock %s: %w", i.obj.GetName(), err)
	}
	if !found {
		return "", fmt.Errorf("affinity not found in Calico IPAMBlock %s", i.obj.GetName())
	}

	// affinity should follow the format "host:node-name"
	affinityParts := strings.Split(affinity, ":")
	if len(affinityParts) != 2 || affinityParts[0] != "host" {
		// this might just be the affinity for something that is not a node
		return "", nil
	}

	return affinityParts[1], nil
}

func (i *calicoIPAMBlock) podCIDRs() ([]string, error) {
	cidr, found, err := unstructured.NestedString(i.obj.Object, "spec", "cidr")
	if err != nil {
		return nil, fmt.Errorf("reading cidr from Calico IPAMBlock %s: %w", i.obj.GetName(), err)
	}
	if !found {
		return nil, fmt.Errorf("cidr not found in Calico IPAMBlock %s", i.obj.GetName())
	}

	return []string{cidr}, nil
}
