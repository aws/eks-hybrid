package pods

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/aws/eks-hybrid/internal/kubelet"
)

const defaultStaticPodManifestPath = "/etc/kubernetes/manifest"

func getPodsOnNode() ([]v1.Pod, error) {
	nodeName, err := kubelet.GetNodeName()
	if err != nil {
		return nil, err
	}

	// Use the current context in the kubeconfig file
	config, err := clientcmd.BuildConfigFromFlags("", kubelet.KubeconfigPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build config from kubeconfig")
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create kubernetes client")
	}

	ctx := context.Background()
	pods, err := clientset.CoreV1().Pods("").List(ctx,
		metav1.ListOptions{
			FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
		},
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list all pods running on the node")
	}

	return pods.Items, nil
}

func ValidateRunningPodsForUninstall() error {
	podsOnNode, err := getPodsOnNode()
	if err != nil {
		return errors.Wrap(err, "failed to get pods on node")
	}

	for _, filter := range getUninstallPodFilters() {
		podsOnNode, err = filter(podsOnNode)
		if err != nil {
			return errors.Wrap(err, "running filter on pods")
		}
	}
	if len(podsOnNode) != 0 {
		return fmt.Errorf("only static pods and pods controlled by daemon-sets can be running on the node. Please move pods " +
			"to different node or provide --skip pod-validation")
	}
	return nil
}
